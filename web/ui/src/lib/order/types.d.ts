
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