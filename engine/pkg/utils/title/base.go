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
	"strings"
	"time"

	"github.com/njtc406/emberengine/engine/pkg/utils/pool"
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

// displayWidth 计算字符串在终端的显示宽度（中文字符算2个宽度）
func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		if r < 128 {
			width++ // ASCII字符宽度1
		} else {
			width += 2 // 非ASCII字符(中文等)宽度2
		}
	}
	return width
}

// padRight 右填充到指定显示宽度
func padRight(s string, width int) string {
	currentWidth := displayWidth(s)
	if currentWidth >= width {
		return s
	}
	return s + strings.Repeat(" ", width-currentWidth)
}

func GracefulExit(elapsed time.Duration, version string) {
	// 打印各种pool的状态信息
	fmt.Printf("%s══════════════ %s ═════════════════%s\n", cyan, translate.Translate("Pool Stats"), reset)
	poolStats := pool.GetPoolStats()
	if poolStats != "" {
		fmt.Println(poolStats)
	} else {
		fmt.Printf(" %s\n", translate.Translate("No pool statistics available"))
	}

	// 获取内存统计
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// 获取CPU核心数
	cores := runtime.NumCPU()
	// 获取当前使用的P数量
	maxProcs := runtime.GOMAXPROCS(0)
	// 获取Goroutine数量
	goroutines := runtime.NumGoroutine()

	// 获取GC统计
	var gcStats debug.GCStats
	debug.ReadGCStats(&gcStats)

	// 计算GC暂停时间统计
	var totalPause, maxPause, avgPause time.Duration
	if len(gcStats.Pause) > 0 {
		maxPause = gcStats.Pause[0]
		for _, p := range gcStats.Pause {
			totalPause += p
			if p > maxPause {
				maxPause = p
			}
		}
		if len(gcStats.Pause) > 0 {
			avgPause = totalPause / time.Duration(len(gcStats.Pause))
		}
	}

	fmt.Printf(" \n%s\n", translate.Translate("Shutting down"))

	// 运行时统计
	fmt.Printf("%s══════════════ %s ═════════════════%s\n", cyan, translate.Translate("Runtime Stats"), reset)
	fmt.Printf(" %s%s%s: %s%.2f%ss\n", dim, padRight(translate.Translate("Uptime"), 20), reset, yellow, elapsed.Seconds(), reset)
	fmt.Printf(" %s%s%s: %s%d%s / %s%d%s (GOMAXPROCS)\n",
		dim, padRight(translate.Translate("CPU"), 20), reset,
		yellow, cores, reset,
		lightCyan, maxProcs, reset)
	fmt.Printf(" %s%s%s: %s%d%s\n", dim, padRight(translate.Translate("Goroutines"), 20), reset, yellow, goroutines, reset)

	// GC统计
	fmt.Printf("%s══════════════ %s ═════════════════%s\n", cyan, translate.Translate("GC Stats"), reset)
	fmt.Printf(" %s%s%s: %s%d%s\n", dim, padRight(translate.Translate("GC Cycles"), 20), reset, yellow, m.NumGC, reset)
	fmt.Printf(" %s%s%s: %s%.3f%sms\n", dim, padRight(translate.Translate("Last GC Pause"), 20), reset, yellow, float64(gcStats.Pause[0])/float64(time.Millisecond), reset)
	fmt.Printf(" %s%s%s: %s%.3f%sms\n", dim, padRight(translate.Translate("Avg GC Pause"), 20), reset, yellow, float64(avgPause)/float64(time.Millisecond), reset)
	fmt.Printf(" %s%s%s: %s%.3f%sms\n", dim, padRight(translate.Translate("Max GC Pause"), 20), reset, yellow, float64(maxPause)/float64(time.Millisecond), reset)
	fmt.Printf(" %s%s%s: %s%.2f%s%%\n", dim, padRight(translate.Translate("GC CPU Fraction"), 20), reset, yellow, m.GCCPUFraction*100, reset)
	fmt.Printf(" %s%s%s: %s%s%s\n", dim, padRight(translate.Translate("Last GC Time"), 20), reset, yellow, gcStats.LastGC.Format("2006-01-02 15:04:05"), reset)

	// 内存统计
	fmt.Printf("%s══════════════ %s ═════════════════%s\n", cyan, translate.Translate("Memory Stats"), reset)
	fmt.Printf(" %s%s%s: %s%.2f%s MB\n", dim, padRight(translate.Translate("Alloc"), 20), reset, yellow, float64(m.Alloc)/1024/1024, reset)
	fmt.Printf(" %s%s%s: %s%.2f%s MB\n", dim, padRight(translate.Translate("TotalAlloc"), 20), reset, yellow, float64(m.TotalAlloc)/1024/1024, reset)
	fmt.Printf(" %s%s%s: %s%.2f%s MB\n", dim, padRight(translate.Translate("Sys"), 20), reset, yellow, float64(m.Sys)/1024/1024, reset)
	fmt.Printf(" %s%s%s: %s%.2f%s MB\n", dim, padRight(translate.Translate("HeapAlloc"), 20), reset, yellow, float64(m.HeapAlloc)/1024/1024, reset)
	fmt.Printf(" %s%s%s: %s%.2f%s MB\n", dim, padRight(translate.Translate("HeapSys"), 20), reset, yellow, float64(m.HeapSys)/1024/1024, reset)
	fmt.Printf(" %s%s%s: %s%.2f%s MB\n", dim, padRight(translate.Translate("HeapIdle"), 20), reset, yellow, float64(m.HeapIdle)/1024/1024, reset)
	fmt.Printf(" %s%s%s: %s%.2f%s MB\n", dim, padRight(translate.Translate("HeapInuse"), 20), reset, yellow, float64(m.HeapInuse)/1024/1024, reset)
	fmt.Printf(" %s%s%s: %s%d%s\n", dim, padRight(translate.Translate("HeapObjects"), 20), reset, yellow, m.HeapObjects, reset)
	fmt.Printf(" %s%s%s: %s%.2f%s MB\n", dim, padRight(translate.Translate("StackInuse"), 20), reset, yellow, float64(m.StackInuse)/1024/1024, reset)

	// 其他统计
	fmt.Printf("%s══════════════ %s ═════════════════%s\n", cyan, translate.Translate("System Info"), reset)
	fmt.Printf(" %s%s%s: %s%s%s\n", dim, padRight(translate.Translate("Go Version"), 20), reset, yellow, runtime.Version(), reset)
	fmt.Printf(" %s%s%s: %s%s%s/%s%s%s\n", dim, padRight(translate.Translate("OS/Arch"), 20), reset, yellow, runtime.GOOS, reset, yellow, runtime.GOARCH, reset)

	fmt.Printf("%s═══════════════════════════════%s\n", cyan, reset)
	fmt.Printf(" %s: y315483585@163.com\n", translate.Translate("Feedback"))
	fmt.Println(" issues: https://github.com/njtc406/emberengine/issues")
	fmt.Printf(" %s%s %sEmber Framework%s v%s%s\n",
		yellow, translate.Translate("Thank you"), lightMagenta, lightCyan, version, reset)
	fmt.Printf("%s═══════════════════════════════%s\n", cyan, reset)
}
