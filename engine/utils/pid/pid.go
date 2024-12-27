// Package pid
// 模块名: 节点进程id
// 功能描述: 进程id
// 作者:  yr  2023/4/22 0022 2:03
// 最后更新:  yr  2023/4/22 0022 2:03
package pid

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"syscall"
)

// RecordPID 记录pid
func RecordPID(cachePath string, nodeID int32, nodeType string) {
	_ = os.WriteFile(path.Join(cachePath, fmt.Sprintf("%s_%d", nodeType, nodeID)+".pid"), ([]byte)(strconv.Itoa(GetPid())), 0644)
}

// DeletePID 删除pid
func DeletePID(cachePath string, nodeID int32, nodeType string) {
	_ = os.Remove(path.Join(cachePath, fmt.Sprintf("%s_%d", nodeType, nodeID)+".pid"))
}

// GetPid 获取pid
func GetPid() int {
	return syscall.Getpid()
}
