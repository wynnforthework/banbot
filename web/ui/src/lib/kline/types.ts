import type {KLineData, Styles, DeepPartial} from 'klinecharts'

export interface SymbolInfo {
  ticker: string
  name?: string
  shortName?: string
  exchange?: string
  market?: string
  pricePrecision?: number
  volumePrecision?: number
  priceCurrency?: string
  type?: string
  logo?: string
}

export type Timespan = 'second' | 'minute' | 'hour' | 'day' | 'week' | 'month'

export interface Period {
  timeframe: string;
  multiplier: number;
  timespan: Timespan;
  secs: number;
}

export type PaneInds = {
  name: string,
  inds: string[]
}

export type KData = {
  data: KLineData[],
  lays?: any[]
}

export type DatafeedWatchCallback = (data: any) => void

export type GetKlineArgs = {
  symbol: SymbolInfo,
  period: Period,
  from: number,
  to: number,
  strategy?: string | null
}

export interface Datafeed {
  getSymbols (): Promise<SymbolInfo[]>
  getHistoryKLineData (args: GetKlineArgs): Promise<KData>
  subscribe (symbol: SymbolInfo, callback: DatafeedWatchCallback): void
  unsubscribe (symbol: SymbolInfo): void
}

export interface ChartProOptions {
  container: string | HTMLElement
  styles?: DeepPartial<Styles>
  watermark?: string | Node
  theme?: string
  locale?: string
  drawingBarVisible?: boolean
  symbol: SymbolInfo
  period: Period
  periods?: Period[]
  timezone?: string
  mainIndicators?: string[]
  subIndicators?: string[]
  datafeed: Datafeed
  persistKey?: string
}

export type PairItem = {
  label: string,
  value: string
}

export interface BotTask{
  task_id: number,
  live: boolean,
  start_ms: number,
  stop_ms: number,
  pairs: string[],
  strategy: string[],
  tfs: string[],
  order_num: number,
  profit_rate: number
}

export interface TaskSymbol{
  symbol: string,
  timeframe: string,
  start_ms: number,
  stop_ms: number,
  order_num: number,
  tot_profit: number,
}

export const OrderStatus = [
  '等待执行',
  '部分完成',
  '已完成'
]

export const InoutStatus = [
  '待执行',
  '部分入场',
  '已入场',
  '部分出场',
  '已平仓'
]

export interface OpenOrder{
  pair: string
  side?: string
  order_type?: string
  price?: number
  stoploss_price?: number
  enter_cost?: number
  enter_tag?: string
  leverage?: number
  strategy?: string
}


export type AddDelInd = {
  is_main: boolean,
  ind_name: string,
  is_add: boolean
}

export type TrendItemType = {
  time: number,
  start_dt: string,
  symbol: string,
  base_s: string,
  quote_s: string,
  rate: number,
  quote_vol: number,
  vol_text: string
}

export type BanInd = {
  name: string,
  title: string,
  cloud: boolean,
  is_main: boolean
}

/**
 * 用于K线图表上显示的交易信息
 * time/price通过Point传入
 */
export interface TradeInfo {
  line_color: string,
  in_color: string,
  in_text: string,
  out_color: string,
  out_text: string,
  active?: boolean,
  selected?: boolean,
  distance?: number
}

export type BarArr = [number, number, number, number, number, number]
