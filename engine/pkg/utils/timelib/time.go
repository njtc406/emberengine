package timelib

import "time"

// Tips: 服务器的所有时间函数都是用time.Local作为时间源,如果需要用到utc或者其他特殊时区,请自行处理并备注清除
// 不要直接使用time.Parse解析时间字符串,请使用time.ParseInLocation解析时间字符串,明确指出使用的时区

var timeOffset int64 = 0 // 服务器时间偏移量

// Now 获取服务器当前时间
func Now() time.Time {
	return time.Now().Add(time.Duration(timeOffset))
}

// GetTimeUnix 获取服务器时间戳
func GetTimeUnix() int64 {
	return time.Now().Add(time.Duration(timeOffset)).Unix()
}

func GetTimeMilli() int64 {
	return time.Now().Add(time.Duration(timeOffset)).UnixMilli()
}

func GetTimeMicro() int64 {
	return time.Now().Add(time.Duration(timeOffset)).UnixMicro()
}

// SetTimeOffset 设置服务器时间偏移量
func SetTimeOffset(offset int64) {
	timeOffset = offset
}

func Since(t time.Time) time.Duration {
	return Now().Sub(t)
}

func SinceFromUnix(t int64) time.Duration {
	return Now().Sub(time.Unix(t, 0))
}

// GetDayStartTime 获取给定时间的当天0点时间
func GetDayStartTime(t time.Time) time.Time {
	if t.IsZero() {
		t = Now()
	}
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.Local)
}

// GetDayStartTimeUnix 获取给定时间的当天0点时间戳
func GetDayStartTimeUnix(t time.Time) int64 {
	return GetDayStartTime(t).Unix()
}

// GetWeekStartTime 获取给定时间的周一0点时间戳
func GetWeekStartTime(t time.Time) time.Time {
	if t.IsZero() {
		t = Now()
	}
	weekDay := int(t.Weekday())
	daysSinceMody := (weekDay + 6) % 7

	monday := t.AddDate(0, 0, -daysSinceMody).Truncate(24 * time.Hour)
	return monday
}

// GetWeekDurationTime 获取给定时间的周一0点-周日23:59:59时间戳
func GetWeekDurationTime(t time.Time) (time.Time, time.Time) {
	if t.IsZero() {
		t = Now()
	}
	weekDay := int(t.Weekday())
	daysSinceMody := (weekDay + 6) % 7

	monday := t.AddDate(0, 0, -daysSinceMody).Truncate(24 * time.Hour)
	nextMonday := monday.AddDate(0, 0, 7).Add(-time.Second)
	return monday, nextMonday
}

// SameWeek 判断两个时间是否在同一周(自然周)
func SameWeek(timestamp1, timestamp2 int64) bool {
	// 将时间戳转换为 time.Time 类型
	time1 := time.Unix(timestamp1, 0)
	time2 := time.Unix(timestamp2, 0)

	// 将时间调整到星期日0点
	sunday1 := time1.AddDate(0, 0, -int(time1.Weekday()))
	sunday2 := time2.AddDate(0, 0, -int(time2.Weekday()))

	// 仅保留日期部分
	year1, month1, day1 := sunday1.Date()
	year2, month2, day2 := sunday2.Date()

	return year1 == year2 && month1 == month2 && day1 == day2
}

func SameDay(timestamp1, timestamp2 int64) bool {
	time1 := time.Unix(timestamp1, 0).Day()
	time2 := time.Unix(timestamp2, 0).Day()
	return time1 == time2
}

// GetMonthStartEndTime 获取给定时间的本月的开时间和结束时间
func GetMonthStartEndTime(t time.Time) (int64, int64) {
	if t.IsZero() {
		t = Now()
	}
	year, month, _ := t.Date()
	startOfMonth := time.Date(year, month, 1, 0, 0, 0, 0, time.Local)
	startOfNextMonth := startOfMonth.AddDate(0, 1, 0)
	endOfMonth := startOfNextMonth.Add(-time.Second)
	return startOfMonth.Unix(), endOfMonth.Unix()
}

// ParseDateTime 解析时间字符串并返回时间戳 格式: YYYY:MM:DD HH:mm:ss
func ParseDateTime(str string) int64 {
	if str == "" {
		return 0
	}
	if t, err := time.ParseInLocation(time.DateTime, str, time.Local); err == nil {
		return t.Unix()
	}
	return 0
}

// ParseDateDay 解析时间字符串并返回时间戳 格式: YYYY:MM:DD
func ParseDateDay(str string) int64 {
	if str == "" {
		return 0
	}
	if t, err := time.ParseInLocation(time.DateOnly, str, time.Local); err == nil {
		return t.Unix()
	}
	return 0
}
