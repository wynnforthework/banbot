
banbot 提供了内置的基于tcp的服务器-客户端数据交换接口，名为BanIO；

源代码：utils/banio.go

支持：断线自动重连、自动数据压缩、订阅&广播机制

主要涉及：ServerIO和ClientIO，一个服务器端可被多个客户端连接，每个连接都是BanConn。

## BanConn
`BanConn`实现的接口如下：
```go
type IBanConn interface {
	WriteMsg(msg *IOMsg) *errs.Error
	Write(data []byte, locked bool) *errs.Error
	ReadMsg() (*IOMsgRaw, *errs.Error)
	
	SetData(val interface{}, tags ...string)
    GetData(key string) (interface{}, bool)
	DeleteData(tags ...string)

    SetWait(key string, size int) string
    GetWaitChan(key string) (chan []byte, bool)
    CloseWaitChan(key string)
    SendWaitRes(key string, data interface{}) *errs.Error
    WaitResult(key string, timeout time.Duration) ([]byte, error)
	
	GetRemote() string
	IsClosed() bool
	RunForever() *errs.Error
}
```
具体结构体如下：
```go
type BanConn struct {
	Conn        net.Conn               // 原始的socket连接
	Data        map[string]interface{} // 消息订阅列表
	Remote      string                 // 远端名称
	Listens     map[string]ConnCB      // 消息处理函数
	RefreshMS   int64                  // 连接就绪的时间戳
	Ready       bool
	IsReading   bool
	DoConnect   func(conn *BanConn) // Reconnect function, no attempt to reconnect provided 重新连接函数，未提供不尝试重新连接
	ReInitConn  func()              // Initialize callback function after successful reconnection 重新连接成功后初始化回调函数
}
```

* `WriteMsg/Write/ReadMsg`用于发送/读取消息到远程目标，用于客户端和服务器消息通信
* `SetData/GetData/DeleteData`用于本地标记此连接的相关信息
* `SetWait/GetWaitChan`等5个方法，用于设置key等待结果写入；用于提交任务到远程后，等待远程返回数据场景
* `RunForever`是持续运行连接，读取解析数据。
* 如果需要修改当前连接在远程端的`Data`，可使用如下方式：
```go
conn.WriteMsg(&IOMsg{Action: "subscribe", Data: []string{"key1"})
conn.WriteMsg(&IOMsg{Action: "unsubscribe", Data: []string{"key1"})
```

## ClientIO
客户端`ClientIO`继承自`BanConn`，额外实现的方法有：
```go
func (c *ClientIO) GetVal(key string, timeout int) (string, *errs.Error)
func (c *ClientIO) SetVal(args *KeyValExpire) *errs.Error
```
`GetVal/SetVal`用于从服务器端设置或读取数据(触发服务器的SetVal/GetVal)


## ServerIO
服务器端`ServerIO`的相关定义如下：
```go
func NewBanServer(addr, name string) *ServerIO
type ServerIO struct {
	Addr     string
	Name     string
	Conns    []IBanConn
	Data     map[string]string // Cache data available for remote access 缓存的数据，可供远程端访问
	DataExp  map[string]int64  // Cache data expiration timestamp, 13 bits 缓存数据的过期时间戳，13位
	InitConn func(*BanConn)
}
func (s *ServerIO) RunForever() *errs.Error
func (s *ServerIO) SetVal(args *KeyValExpire)
func (s *ServerIO) GetVal(key string) string
func (s *ServerIO) Broadcast(msg *IOMsg) *errs.Error
func (s *ServerIO) WrapConn(conn net.Conn) *BanConn
```
`Broadcast`遍历所有连接，对所有`GetData(msg.Action)`有数据的连接，视为已订阅，发送数据
实现自定义Server时，一般需要传入`InitConn`，处理客户端发送的额外Action

## 场景说明
* 客户端一次性订阅服务器数据，服务器周期性推送；客户端可在建立连接后`WriteMsg`，发送subscribe；服务器定期调用Broadcast推送到多个客户端即可
* 服务器发送任务到某个客户端执行，等待执行结果：服务器调用`SetWait`设置RequestID，然后`WriteMsg`发送任务信息(带RequestID)，然后可`WaitResult`获取结果。客户端完成任务后调用`SendWaitRes`发送RequestID对应结果，服务器`WaitResult`被触发收到任务结果。
