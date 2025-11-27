/*
 * Copyright (c) 2023. YR. All rights reserved
 */

// Package translate
// 模块名: 模块名
// 功能描述: 描述
// 作者:  yr  2023/4/26 0026 23:01
// 最后更新:  yr  2023/4/26 0026 23:01
package translate

func init() {
	Register(ZH_CN, zhCnMap)
}

var zhCnMap = map[string]string{
	"Press enter key to exit...":   "按回车键退出...",    // 回车键退出
	"Version":                      "版本号",          // 版本
	"Powered by":                   "由Ember框架提供支持", // 提供支持
	"Thank you":                    "感谢使用",         // 感谢使用
	"Shutting down":                "程序关闭",         // 关机中
	"Uptime":                       "运行时长",         // 运行时间
	"Memory usage":                 "内存占用",         // 内存使用情况
	"Feedback":                     "问题反馈",         // 反馈
	"Runtime Stats":                "运行统计",         // 运行统计
	"Memory Stats":                 "内存统计",         // 内存统计
	"GC Stats":                     "垃圾回收统计",       // GC统计
	"System Info":                  "系统信息",         // 系统信息
	"CPU Cores":                    "CPU核心数",       // CPU核心数
	"CPU":                          "CPU",          // CPU
	"HeapAlloc":                    "堆分配",          // 堆分配
	"GC Cycles":                    "GC次数",         // GC次数
	"Last GC Pause":                "上次GC耗时",       // 上次GC暂停时间
	"Avg GC Pause":                 "平均GC耗时",       // 平均GC暂停时间
	"Max GC Pause":                 "最大GC耗时",       // 最大GC暂停时间
	"GC CPU Fraction":              "GC CPU占用",     // GC CPU占比
	"Last GC Time":                 "上次GC时间",       // 上次GC时间
	"Goroutines":                   "活跃协程数量",       // 活跃协程数量
	"Alloc":                        "当前分配",         // 当前分配内存
	"TotalAlloc":                   "累计分配",         // 累计分配内存
	"Sys":                          "系统占用",         // 系统内存
	"HeapSys":                      "堆系统内存",        // 堆系统内存
	"HeapIdle":                     "堆空闲内存",        // 堆空闲内存
	"HeapInuse":                    "堆使用内存",        // 堆使用内存
	"HeapObjects":                  "堆对象数",         // 堆对象数
	"StackInuse":                   "栈使用内存",        // 栈使用内存
	"Go Version":                   "Go版本",         // Go版本
	"OS/Arch":                      "操作系统/架构",      // 操作系统/架构
	"Pool Stats":                   "对象池统计",        // 对象池统计
	"No pool statistics available": "暂无对象池统计数据",    // 暂无对象池统计
}
