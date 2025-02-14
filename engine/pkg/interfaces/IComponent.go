// Package interfaces
// @Title  title
// @Description  desc
// @Author  yr  2025/2/13
// @Update  yr  2025/2/13
package interfaces

type IComponent interface {
	// Start 启动
	Start() error
	// Stop 停止
	Stop()
}

// TODO 所有的需要启动的接口都继承这个接口,然后在自己的里面实现特异化的其他接口？
