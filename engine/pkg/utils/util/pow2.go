package util

import "math/bits"

// RoundUpToPowerOfTwoInt 向上取整到 2 的幂次方。
// v <= 1 时返回 1。
// 注意：返回值受 int 的位宽限制（32/64位）。
func RoundUpToPowerOfTwoInt(v int) int {
	if v <= 1 {
		return 1
	}
	// 对于有符号 int，最大可表示的 2 的幂为 1<<(bits.UintSize-2)
	maxPow2 := 1 << (bits.UintSize - 2)
	if v >= maxPow2 {
		return maxPow2
	}
	return 1 << bits.Len(uint(v-1))
}

// RoundUpToPowerOfTwoInt64 向上取整到 2 的幂次方。
// v <= 1 时返回 1。
func RoundUpToPowerOfTwoInt64(v int64) int64 {
	if v <= 1 {
		return 1
	}
	// math.MaxInt64 的最大 2 的幂为 1<<62
	const maxPow2 int64 = 1 << 62
	if v >= maxPow2 {
		return maxPow2
	}
	return int64(1) << bits.Len64(uint64(v-1))
}
