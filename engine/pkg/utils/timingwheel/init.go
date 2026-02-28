// Package timingwheel
// 模块名: timingwheel
// 说明: 定时器时间轮模块
// 作者: yr
// 更新: 2025/1/13
package timingwheel

// cronParser 是包级只读解析器（白名单 — 无状态，init 后不可变）。
var cronParser Parser

func init() {
	// 格式: 秒,分,时,日,月,周
	cronParser = NewParser(Second | Minute | Hour | Dom | Month | Dow | DowOptional | Descriptor)
}

// 以下全局变量和全局函数已删除（Node 自包含改造 Phase 1）:
//   - var globTW *TimingWheel
//   - var twMutex sync.Mutex
//   - func Start(...)
//   - func Stop()
//   - func GetTimingWheel() *TimingWheel
//   - func SetTimeOffset(offset time.Duration)
//
// 所有调用方应通过 Node 持有的 *TimingWheel 实例直接操作。
