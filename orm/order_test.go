package orm

import "testing"

func TestGetOrders(t *testing.T) {
	err := initApp()
	if err != nil {
		panic(err)
	}
	sess, conn, err := Conn(nil)
	if err != nil {
		panic(err)
	}
	defer conn.Release()
	sess.GetOrders(GetOrdersArgs{})
}
