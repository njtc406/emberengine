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
	"Press enter key to exit...":   "Press enter key to exit...",   // 回车键退出
	"Version":                      "Version",                      // 版本
	"Powered by":                   "Powered by Ember Framework",   // 由xxx提供支持
	"Thank you":                    "Thank you for using",          // 感谢使用
	"Shutting down":                "Shutting down",                // 关机中
	"Uptime":                       "Uptime",                       // 运行时间
	"Memory usage":                 "Memory usage",                 // 内存使用情况
	"Feedback":                     "Feedback",                     // 反馈
	"Runtime Stats":                "Runtime Stats",                // 运行统计
	"Memory Stats":                 "Memory Stats",                 // 内存统计
	"GC Stats":                     "GC Stats",                     // GC统计
	"System Info":                  "System Info",                  // 系统信息
	"CPU Cores":                    "CPU Cores",                    // CPU核心数
	"CPU":                          "CPU",                          // CPU
	"HeapAlloc":                    "HeapAlloc",                    // 堆分配
	"GC Cycles":                    "GC Cycles",                    // GC次数
	"Last GC Pause":                "Last GC Pause",                // 上次GC暂停
	"Avg GC Pause":                 "Avg GC Pause",                 // 平均GC暂停
	"Max GC Pause":                 "Max GC Pause",                 // 最大GC暂停
	"GC CPU Fraction":              "GC CPU Fraction",              // GC CPU占比
	"Last GC Time":                 "Last GC Time",                 // 上次GC时间
	"Goroutines":                   "Goroutines",                   // 协程数
	"Alloc":                        "Alloc",                        // 当前分配
	"TotalAlloc":                   "TotalAlloc",                   // 累计分配
	"Sys":                          "Sys",                          // 系统内存
	"HeapSys":                      "HeapSys",                      // 堆系统内存
	"HeapIdle":                     "HeapIdle",                     // 堆空闲
	"HeapInuse":                    "HeapInuse",                    // 堆使用
	"HeapObjects":                  "HeapObjects",                  // 堆对象数
	"StackInuse":                   "StackInuse",                   // 栈使用
	"Go Version":                   "Go Version",                   // Go版本
	"OS/Arch":                      "OS/Arch",                      // 操作系统/架构
	"Pool Stats":                   "Pool Stats",                   // 对象池统计
	"No pool statistics available": "No pool statistics available", // 暂无对象池统计
}
