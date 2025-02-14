// Package wsmodule
// @Title  请填写文件名称（需要改）
// @Description  请填写文件描述（需要改）
// @Author  yr  2025/1/21 上午10:33
// @Update  yr  2024/1/21 上午10:33
package wsmodule

type MsgProcessor interface {
	Unmarshal(msg []byte) (interface{}, error)
	Marshal(msg interface{}) ([]byte, error)
}

type Handler interface {
	Handle(client *Client, msg interface{}) error
}
