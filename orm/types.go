package orm

type InOutOrder struct {
	Main  *Iorder
	Enter *Exorder
	Exit  *Exorder
	Info  map[string]interface{}
}

const (
	OdStopLoss = "StopLossPrice"
)
