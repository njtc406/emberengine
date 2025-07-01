/*
 * Copyright (c) 2023. YR. All rights reserved
 */

// Package translate
// 模块名: 英文翻译
// 功能描述: 对应字符串转换为英文
// 作者:  yr  2023/4/26 0026 23:00
// 最后更新:  yr  2023/4/26 0026 23:00
package translate

func init() {
	Register(EN_US, enUsMap)
}

var enUsMap = map[string]string{
	"Press enter key to exit...": "Press enter key to exit...", // 回车键退出
	"Version":                    "Version",                    // 版本
	"Powered by":                 "Powered by Ember Framework", // 由xxx提供支持
	"Thank you":                  "Thank you for using",        // 感谢使用
	"Shutting down":              "Shutting down",              // 关机中
	"Uptime":                     "Uptime",                     // 运行时间
	"Memory usage":               "Memory usage",               // 内存使用情况
	"Feedback":                   "Feedback",                   // 反馈
	"Runtime Stats":              "Runtime Stats",
	"Memory Stats":               "Memory Stats",
	"CPU Cores":                  "CPU Cores",
	"HeapAlloc":                  "HeapAlloc",
	"GC Cycles":                  "GC Cycles",
	"Last GC Pause":              "Last GC Pause",
	"Goroutines":                 "Goroutines",
}
