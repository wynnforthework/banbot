import type {IndicatorTemplate} from 'klinecharts';
import {postApi} from "../../netio";
import {drawCloudInd} from "./common";
import {IndFieldsMap} from "../coms"
import * as m from "$lib/paraglide/messages";

const param = m.param();
/**
 * 按传入的参数生成云端指标。
 * 支持自定义图形：
 * tag: 买卖点显示，做多时值为正数的价格，做空时值为负数的价格。
 * @param params
 */
export const makeCloudInds = (params: Record<string, any>[]): IndicatorTemplate[] => {
  return params.map((args): IndicatorTemplate => {
    const name = args['name']
    const figures = args['figures'] ?? []
    const calcParams = args['calcParams'] ?? [];
    const figureTpl = args['figure_tpl'];
    if (calcParams.length > 0){
      let fields: Array<any> = []
      calcParams.forEach((v: any, i: number) => {
        if (figureTpl){
          let key = figureTpl.replace(/\{i\}/, i+1)
          let plot_type = args['figure_type'];
          if (!plot_type){
            plot_type = 'line'
          }
          figures.push({key, title: `${key.toUpperCase()}: `, type: plot_type})
        }
        fields.push({ title: param + (i+1), precision: 0, min: 1, styleKey: `lines[${i}].color`, default: v })
      })
      IndFieldsMap[name] = fields
    }
    return {
      ...args, name, figures,
      calc: async (dataList, ind) => {
        const name = ind.name;
        const params = ind.calcParams;
        const kwargs = ind.extendData;
        const kline = dataList.map(d => [d.timestamp, d.open, d.high, d.low, d.close, d.volume]);
        if (kline.length == 0){return []}
        const rsp = await postApi('/kline/calc_ind', {name, params, kline, kwargs})
        if (rsp.code != 200 || !rsp.data) {
          console.error('calc ind fail:', rsp)
          return dataList.map(d => {
            return {}
          })
        }
        return rsp.data ?? []
      },
      draw: drawCloudInd
    }
  })
}

export default makeCloudInds
