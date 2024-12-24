import * as m from '$lib/paraglide/messages.js';

export interface GroupItem {
  title: string;
  winCount: number;
  orderNum: number;
  profitPctSum: number;
  profitSum: number;
  costSum: number;
  durations: number[];
  sharpe: number;
  sortino: number;
}

export interface BackTestPlots {
  available: number[];
  jobNum: number;
  labels: string[];
  odNum: number[];
  profit: number[];
  real: number[];
  unrealizedPOL: number[];
  withDraw: number[];
}

export interface BacktestDetail {
  pairGrps: GroupItem[];
  dateGrps: GroupItem[];
  profitGrps: GroupItem[];
  enterGrps: GroupItem[];
  exitGrps: GroupItem[];
  plots: BackTestPlots;
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

export interface BackTestTask {
  id: number;
  mode: string;
  status: number;
  pairs: string;
  periods: string;
  strats: string;
  config: string;
  args: string;
  path: string;
  createAt: number;
  startAt: number;
  stopAt: number;
  leverage: number;
  stakeAmount: number;
  walletAmount: number;
  totalInvest: number;
  barNum: number;
  orderNum: number;
  maxOpenOrders: number;
  totProfit: number;
  totProfitPct: number;
  profitRate: number;
  winRate: number;
  maxDrawdown: number;
  maxDrawDownVal: number;
  showDrawDownPct: number;
  showDrawDownVal: number;
  sharpe: number;
  sortinoRatio: number;
  totCost: number;
  totFee: number;
}

export interface ExSymbol {
  id: number;
  exchange: string;
  exg_real: string;
  market: string;
  symbol: string;
  combined: boolean;
  list_ms: number;
  delist_ms: number;
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