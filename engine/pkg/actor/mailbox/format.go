package mailbox

// itoa 无分配整数格式化（支持负数）
func itoa(n int) string {
	if n < 0 {
		return "-" + itoaU64(uint64(-n))
	}
	return itoaU64(uint64(n))
}

// itoaU64 无分配 uint64 格式化
func itoaU64(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// formatPct 格式化百分比（1 位小数），避免 fmt 开销
func formatPct(p float64) string {
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	whole := int(p)
	frac := int((p - float64(whole)) * 10)
	return itoa(whole) + "." + itoa(frac)
}

// formatFloat1 格式化浮点数（1 位小数，支持负数），避免 fmt 开销
func formatFloat1(v float64) string {
	neg := false
	if v < 0 {
		neg = true
		v = -v
	}
	whole := int(v)
	frac := int((v - float64(whole)) * 10)
	s := itoa(whole) + "." + itoa(frac)
	if neg {
		return "-" + s
	}
	return s
}
