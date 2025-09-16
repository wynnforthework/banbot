import * as m from '$lib/paraglide/messages.js';

export const exchanges: string[] = ['binance', 'bybit', 'china'];

export function getMarkets() {
  return [
    { title: m.market_spot(), value: 'spot' },
    { title: m.market_linear(), value: 'linear' },
    { title: m.market_inverse(), value: 'inverse' },
    { title: m.market_option(), value: 'option' }
  ];
}

// 保持向后兼容性
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

/**
 * 格式化价格显示
 * @param value 要格式化的数字
 * @param digits 有效数字位数，默认6位
 * @returns 格式化后的字符串
 *
 * 规则：
 * - < 0.001: 使用科学计数法，保留指定位有效数字（截断）
 * - 0.001 ~ 1000: 保留指定位有效数字（截断）
 * - >= 1000: 使用千位分隔符，保留指定位有效数字（截断）
 */
export function fmtNumber(value: number, digits: number = 6): string {
  // 处理特殊值
  if (value === 0) return '0';
  if (!isFinite(value) || isNaN(value)) return '0';

  const absValue = Math.abs(value);
  const sign = value < 0 ? '-' : '';

  // < 0.001: 使用科学计数法
  if (absValue < 0.001) {
    return sign + formatScientific(absValue, digits);
  }

  // 0.001 ~ 1000: 保留有效数字，不使用千位分隔符
  if (absValue < 1000) {
    return sign + formatWithDigits(absValue, digits);
  }

  // >= 1000: 使用千位分隔符
  // 如果是整数或接近整数，保留整数部分
  const integerPart = Math.floor(absValue);
  const decimalPart = absValue - integerPart;

  // 如果小数部分很小或整数部分已经很大，只显示整数
  if (decimalPart < 0.01 || integerPart >= 1000000) {
    return sign + addThousandsSeparator(integerPart.toString());
  }

  // 否则按有效数字格式化
  const formatted = formatWithDigits(absValue, digits);
  return sign + addThousandsSeparator(formatted);
}

/**
 * 格式化科学计数法（截断）
 */
function formatScientific(value: number, digits: number): string {
  // 获取指数
  const exponent = Math.floor(Math.log10(value));
  // 获取尾数
  const mantissa = value / Math.pow(10, exponent);
  // 截断尾数到指定位数
  const factor = Math.pow(10, digits - 1);
  const truncatedMantissa = Math.floor(mantissa * factor) / factor;

  // 格式化输出，保持固定的小数位数
  const mantissaStr = truncatedMantissa.toFixed(digits - 1);
  return `${mantissaStr}e${exponent}`;
}

/**
 * 按有效数字格式化数字（截断而非四舍五入）
 */
function formatWithDigits(value: number, digits: number): string {
  if (value === 0) return '0';

  // 计算整数部分的位数
  const integerDigits = Math.floor(Math.log10(value)) + 1;

  // 如果整数部分位数超过有效数字，优先保留整数
  if (integerDigits >= digits) {
    // 对于超大数字，保留整数部分
    if (integerDigits > digits) {
      const factor = Math.pow(10, integerDigits - digits);
      const truncated = Math.floor(value / factor) * factor;
      return truncated.toString();
    }
    // 整数部分刚好等于有效数字，返回整数
    return Math.floor(value).toString();
  }

  // 计算需要保留的小数位数
  const decimalPlaces = digits - integerDigits;
  const factor = Math.pow(10, decimalPlaces);
  const truncated = Math.floor(value * factor) / factor;

  // 转换为字符串并移除末尾的零
  let result = truncated.toFixed(decimalPlaces);
  if (result.indexOf('.') >= 0) {
    result = result.replace(/\.?0+$/, '');
  }
  return result;
}

/**
 * 添加千位分隔符
 */
function addThousandsSeparator(value: string): string {
  const parts = value.split('.');
  const integerPart = parts[0];
  const decimalPart = parts[1];

  // 为整数部分添加千位分隔符
  const withSeparator = integerPart.replace(/\B(?=(\d{3})+(?!\d))/g, ',');

  // 如果有小数部分，拼接回去
  return decimalPart !== undefined ? `${withSeparator}.${decimalPart}` : withSeparator;
}

