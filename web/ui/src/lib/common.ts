export const exchanges: string[] = ['binance', 'bybit', 'china'];
export const markets: string[] = ['spot', 'linear', 'inverse', 'option'];
export const periods: string[] = ['1m', '5m', '15m', '1h', '1d'];

export function getFirstValid(vals: any[]){
  for(let i = 0; i < vals.length; i++){
    let val = vals[i];
    if(val){
      return val;
    }
  }
  return 0
}

export interface StrVal{
  str: string
  val: any
}