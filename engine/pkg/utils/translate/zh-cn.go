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
	"Press enter key to exit...": "按回车键退出...",    // 回车键退出
	"Version":                    "版本号",          // 版本
	"Powered by":                 "由Ember框架提供支持", // 提供支持
	"Thank you":                  "感谢使用",         // 感谢使用
	"Shutting down":              "程序关闭",         // 关机中
	"Uptime":                     "运行时长",         // 运行时间
	"Memory usage":               "内存占用",         // 内存使用情况
	"Feedback":                   "问题反馈",         // 反馈
	"Runtime Stats":              "运行统计",         // 运行统计
	"Memory Stats":               "内存统计",
	"CPU Cores":                  "CPU核心数",
	"HeapAlloc":                  "堆分配",
	"GC Cycles":                  "GC次数",
	"Last GC Pause":              "上次GC耗时",
	"Goroutines":                 "线程数",
}
