package timingwheel

import "time"

// ConstantDelaySchedule 表示简单的重复调度，例如“每 5 分钟执行一次”。
// 不支持频率高于每秒一次的任务。
type ConstantDelaySchedule struct {
	Delay time.Duration
}

// Every 返回一个按指定间隔重复激活的调度。
// 小于 1 秒的间隔不支持（会向上取整为 1 秒）。
// 所有小于秒的字段会被截断。
func Every(duration time.Duration) ConstantDelaySchedule {
	if duration < time.Second {
		duration = time.Second
	}
	return ConstantDelaySchedule{
		Delay: duration - time.Duration(duration.Nanoseconds())%time.Second,
	}
}

// Next 返回下一次应执行的时间，结果会向秒对齐。
func (schedule ConstantDelaySchedule) Next(t time.Time) time.Time {
	return t.Add(schedule.Delay - time.Duration(t.Nanosecond())*time.Nanosecond)
}
