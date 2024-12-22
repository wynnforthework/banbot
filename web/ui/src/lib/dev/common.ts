import * as m from '$lib/paraglide/messages.js';

export interface Order {
  id: number;
  symbol: string;
  short: boolean;
  leverage: number;
  enter_at: number;
  exit_at: number;
  enter_tag: string;
  exit_tag: string;
  enter_price?: number;
  enter_average?: number;
  enter_amount?: number;
  exit_price?: number;
  exit_average?: number;
  exit_amount?: number;
  exit_filled?: number;
  profit: number;
  profit_rate: number;
  timeframe: string;
  strategy: string;
  stg_ver: string;
  init_price?: number;
  info?: any;
  status: number;
}

export interface GroupItem {
  title: string;
  Orders: Order[];
  WinCount: number;
  profitSum: number;
  costSum: number;
  durations: number[];
  sharpe: number;
  sortino: number;
}

export interface BacktestDetail {
  pairGrps: GroupItem[];
  dateGrps: GroupItem[];
  profitGrps: GroupItem[];
  enterGrps: GroupItem[];
  exitGrps: GroupItem[];
  totProfit: number;
  totProfitPct: number;
  winRatePct: number;
  orderNum: number;
  maxDrawDownPct: number;
  maxDrawDownVal: number;
  sharpeRatio: number;
  sortinoRatio: number;
  startMS: number;
  endMS: number;
  totalInvest: number;
  maxReal: number;
  totFee: number;
  barNum: number;
  maxOpenOrders: number;
  totCost: number;
  showDrawDownPct: number;
  showDrawDownVal: number;
}

export function showPairs(pairs: string) {
  if(!pairs) return '';
  const symbols = ' ' + m.symbols();
  if(pairs.startsWith('num_')){
    return pairs.substring(4) + symbols;
  }else if (pairs.startsWith('top_')){
    return 'Top' +pairs.substring(4) + symbols;
  }else{
    return pairs;
  }
}