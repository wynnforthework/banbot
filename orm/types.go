package orm

type InOutOrder struct {
	Main  *IOrder
	Enter *ExOrder
	Exit  *ExOrder
	Info  map[string]interface{}
}

type KlineAgg struct {
	TimeFrame string
	Secs      int64
	Table     string
	AggFrom   string
	AggStart  string
	AggEnd    string
	AggEvery  string
	CpsBefore string
	Retention string
}

const (
	OdInfoStopLoss  = "StopLossPrice"
	OdInfoLegalCost = "LegalCost"
)
