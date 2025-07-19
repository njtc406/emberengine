// Package version
// Mode Module: 模块名
// Mode Desc: 模块功能描述
package version

var Version = "2.1.0"

// CompareVersion 返回 1 表示 v1 > v2（即 v1 更新），
// 返回 -1 表示 v1 < v2（即 v2 更新），返回 0 表示两者相等。
func CompareVersion(v1, v2 string) int {
	i, j := 0, 0
	n1, n2 := len(v1), len(v2)
	for i < n1 || j < n2 {
		num1, num2 := 0, 0

		// 解析 v1 中当前的数字段
		for i < n1 && v1[i] != '.' {
			// 假设版本号中都是合法的数字字符
			num1 = num1*10 + int(v1[i]-'0')
			i++
		}

		// 解析 v2 中当前的数字段
		for j < n2 && v2[j] != '.' {
			num2 = num2*10 + int(v2[j]-'0')
			j++
		}

		// 比较当前段数字
		if num1 > num2 {
			return 1
		} else if num1 < num2 {
			return -1
		}

		// 跳过分隔符 "."
		i++
		j++
	}
	return 0
}

// IsVersionSufficient 返回 true 当 current 版本大于等于 minVersion 时。
func IsVersionSufficient(current, minVersion string) bool {
	return CompareVersion(current, minVersion) >= 0
}
