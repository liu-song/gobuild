// Copyright 2015 by caixw, All rights reserved.
// Use of this source code is governed by a MIT
// license that can be found in the LICENSE file.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/fsnotify.v1"
)

type builder struct {
	exts      []string  // 不需要监视的文件扩展名
	appCmd    *exec.Cmd // 被编译的程序
	goCmdArgs []string  // 传递给go build的参数
}

// 初始化一个builder实例。
//
// mainFiles 需要被编译的文件名，可以使用通配符；
// outputName 被编译之后，保存的文件名，可以带路径，windows下若不带".exe"会自动加上；
// exts 需要被监视的扩展名列表；
// paths 需要被监视的路径列表，可以是文件，也可以是文件夹。
func newBuilder(mainFiles, outputName string, exts, paths []string) *builder {
	wd, err := os.Getwd()
	if err != nil {
		log(erro, "获取当前工作目录时，发生以下错误:", err)
		os.Exit(2)
	}

	// 确定编译后的文件名
	if len(outputName) == 0 {
		outputName = filepath.Base(wd)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(outputName, ".exe") {
		outputName += ".exe"
	}
	if strings.IndexByte(outputName, '/') < 0 || strings.IndexByte(outputName, filepath.Separator) < 0 {
		outputName = wd + string(filepath.Separator) + outputName
	}

	// 初始化apCmd变量
	appCmd := exec.Command(outputName)
	appCmd.Stderr = os.Stderr
	appCmd.Stdout = os.Stdout

	// 初始化goCmd的参数
	args := []string{"build", "-o", outputName}
	if len(mainFiles) > 0 {
		args = append(args, mainFiles)
	}

	// 去除无效的扩展名
	newExts := make([]string, 0, len(exts))
	for _, ext := range exts {
		if len(ext) == 0 {
			continue
		}
		if ext[0] != '.' {
			ext = "." + ext
		}
		newExts = append(newExts, ext)
	}

	b := &builder{
		exts:      newExts,
		appCmd:    appCmd,
		goCmdArgs: args,
	}
	b.watch(append(paths, wd))
	return b
}

// 确定文件path是否属于被忽略的格式。
func (b *builder) isIgnore(path string) bool {
	if b.appCmd != nil && b.appCmd.Path == path { // 忽略程序本身的监视
		return true
	}

	for _, ext := range b.exts {
		if len(ext) == 0 {
			continue
		}
		if ext == "*" {
			return false
		}
		if strings.HasSuffix(path, ext) {
			return false
		}
	}

	return true
}

// 开始编译代码
func (b *builder) build() {
	log(info, "编译代码...")

	goCmd := exec.Command("go", b.goCmdArgs...)
	goCmd.Stderr = os.Stderr
	goCmd.Stdout = os.Stdout
	if err := goCmd.Run(); err != nil {
		log(erro, "编译失败:", err)
		return
	}

	log(succ, "编译成功!")

	b.restart()
}

// 重启被编译的程序
func (b *builder) restart() {
	defer func() {
		if err := recover(); err != nil {
			log(erro, "restart.defer:", err)
		}
	}()

	// kill process
	if b.appCmd != nil && b.appCmd.Process != nil {
		log(info, "中止旧进程...")
		if err := b.appCmd.Process.Kill(); err != nil {
			log(erro, "kill:", err)
		}
	}

	if err := b.appCmd.Run(); err != nil {
		log(erro, "启动进程时出错:", err)
	}
}

// 开始监视paths中指定的目录或文件。
func (b *builder) watch(paths []string) {
	log(info, "初始化监视器...")

	// 初始化监视器
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log(erro, err)
		os.Exit(2)
	}

	// 监视的路径，必定包含当前工作目录
	log(info, "以下路径或是文件将被监视:", paths)
	for _, path := range paths {
		if err := watcher.Add(path); err != nil {
			log(erro, "watcher.Add:", err)
			os.Exit(2)
		}
	}

	go func() {
		var buildTime int64
		for {
			select {
			case event := <-watcher.Events:
				if event.Op&fsnotify.Chmod == fsnotify.Chmod {
					log(ignore, "watcher.Events:忽略CHMOD事件:", event)
					continue
				}

				if b.isIgnore(event.Name) { // 不需要监视的扩展名
					log(ignore, "watcher.Events:忽略不被监视的文件:", event)
					continue
				}

				if time.Now().Unix()-buildTime <= 1 { // 已经记录
					log(ignore, "watcher.Events:该监控事件被忽略:", event)
					continue
				}

				buildTime = time.Now().Unix()
				log(info, "watcher.Events:触发编译事件:", event)

				go b.build()
			case err := <-watcher.Errors:
				log(warn, "watcher.Errors", err)
			}
		}
	}()
}
