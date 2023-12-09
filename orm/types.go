package orm

type InOutOrder struct {
	Main  *IOrder
	Enter *ExOrder
	Exit  *ExOrder
	Info  map[string]interface{}
}

const (
	OdInfoStopLoss  = "StopLossPrice"
	OdInfoLegalCost = "LegalCost"
)
