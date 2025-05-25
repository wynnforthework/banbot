
export interface ExOrder {
  id: number;
  task_id: number;
  inout_id: number;
  symbol: string;
  enter: boolean;
  order_type: string;
  order_id: string;
  side: string;
  create_at: number;
  price: number;
  average: number;
  amount: number;
  filled: number;
  status: number;
  fee: number;
  fee_type: string;
  update_at: number;
}

export interface IOrder {
  id: number;
  task_id: number;
  symbol: string;
  sid: number;
  timeframe: string;
  short: boolean;
  status: number;
  enter_tag: string;
  init_price: number;
  quote_cost: number;
  exit_tag: string;
  leverage: number;
  enter_at: number;
  exit_at: number;
  strategy: string;
  stg_ver: number;
  max_pft_rate: number;
  max_draw_down: number;
  profit_rate: number;
  profit: number;
}

export interface InOutOrder extends IOrder {
  enter?: ExOrder;
  exit?: ExOrder;
  info?: Record<string, any>;
  curPrice?: number;
}

export interface BanExgOrder {
  info: Record<string, any>;
  id: string;
  clientOrderId: string;
  datetime: string;
  timestamp: number;
  lastTradeTimestamp: number;
  lastUpdateTimestamp: number;
  status: string;
  symbol: string;
  type: string;
  timeInForce: string;
  positionSide: string;
  side: string;
  price: number;
  average: number;
  amount: number;
  filled: number;
  remaining: number;
  triggerPrice: number;
  stopPrice: number;
  takeProfitPrice: number;
  stopLossPrice: number;
  cost: number;
  postOnly: boolean;
  reduceOnly: boolean;
  trades: BanTrade[];
  fee: Fee;
}

export interface BanTrade{
  id: string;
  symbol: string;
  side: string;
  type: string;
  amount: number;
  price: number;
  cost: number;
  order: string;
  timestamp: number;
  maker: boolean;
  fee: Fee;
  info: Record<string, any>;
}

export interface Fee {
  isMaker: boolean;
  currency: string;
  cost: number;
  rate: number;
}

export interface Position{
  id: string;
  symbol: string;
  timestamp: number;
  isolated: boolean;
  hedged: boolean;
  side: string;
  contracts: number;
  contractSize: number;
  entryPrice: number;
  markPrice: number;
  notional: number;
  leverage: number;
  collateral: number;
  initialMargin: number;
  maintenanceMargin: number;
  initialMarginPercentage: number;
  maintenanceMarginPercentage: number;
  unrealizedPnl: number;
  liquidationPrice: number;
  marginMode: string;
  marginRatio: number;
  percentage: number;
  info: Record<string, any>;
}

export interface OdGroup {
  key: string;
  holdHours: number;
  totalCost: number;
  closeNum: number;
  winNum: number;
  profitSum: number;
  profitPct: number;
  nums: number[];
  minTime: number;
  maxTime: number;
}