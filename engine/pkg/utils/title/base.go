/*
 * Copyright (c) 2023. YR. All rights reserved
 */

// Package title
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2023/4/26 0026 22:51
// 最后更新:  yr  2023/4/26 0026 22:51
package title

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/utils/translate"
)

const (
	reset        = "\033[0m"
	cyan         = "\033[36m"
	yellow       = "\033[33m"
	magenta      = "\033[35m"
	bold         = "\033[1m"
	dim          = "\033[2m"
	lightMagenta = "\033[38;5;13m"
	lightYellow  = "\033[38;5;11m"
	lightCyan    = "\033[38;5;12m"
)

func EchoTitle(version string) {
	fmt.Print(fmt.Sprintf(titleBase, translate.Translate("Powered by"), translate.Translate("Version"), version))
}

func GracefulExit(elapsed time.Duration, version string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 获取CPU核心数
	cores := runtime.NumCPU()
	// 获取Goroutine数量
	goroutines := runtime.NumGoroutine()
	// 获取GC统计
	var gcStats debug.GCStats
	debug.ReadGCStats(&gcStats)

	fmt.Printf(" \n%s\n", translate.Translate("Shutting down"))
	fmt.Printf("%s══════════════ %s ═════════════════%s\n", cyan, translate.Translate("Runtime Stats"), reset)
	fmt.Printf(" %s: %.1fs\n", translate.Translate("Uptime"), elapsed.Seconds())
	fmt.Printf(" %s: %d (%s: %d)\n", translate.Translate("CPU Cores"), cores, translate.Translate("Goroutines"), goroutines)
	fmt.Printf(" %s: %d\n", translate.Translate("GC Cycles"), m.NumGC)
	fmt.Printf(" %s: %.2fms\n", translate.Translate("Last GC Pause"), float64(gcStats.Pause[0])/float64(time.Millisecond))
	fmt.Printf("%s══════════════ %s ═════════════════%s\n", cyan, translate.Translate("Memory Stats"), reset)
	fmt.Printf(" %s: %.2fMB\n", translate.Translate("Memory usage"), float64(m.Alloc)/1024/1024)
	fmt.Printf(" %s: %.2f MB\n", translate.Translate("HeapAlloc"), float64(m.HeapAlloc)/1024/1024)
	fmt.Printf("%s═══════════════════════════════%s\n", cyan, reset)
	fmt.Printf(" %s: y315483585@163.com\n", translate.Translate("Feedback"))
	fmt.Println(" issues: https://github.com/njtc406/emberengine/issues")
	fmt.Printf(" %s%s %sEmber Framework%s v%s%s\n",
		yellow, translate.Translate("Thank you"), lightMagenta, lightCyan, version, reset)
	fmt.Printf("%s═══════════════════════════════%s\n", cyan, reset)
}
