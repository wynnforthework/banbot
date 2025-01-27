<script lang="ts">
  import { onMount } from 'svelte';
  import { page } from '$app/stores';
  import Chart from '$lib/kline/Chart.svelte';
  import { getAccApi } from '$lib/netio';
  import { alerts } from '$lib/stores/alerts';
  import type { OverlayCreate } from 'klinecharts';
  import { fmtDuration, fmtDateStr } from '$lib/dateutil';
  import * as m from '$lib/paraglide/messages';
  import { derived } from 'svelte/store';
  import { ChartCtx, ChartSave } from '$lib/kline/chart';
  import { persisted } from 'svelte-persisted-store';
  import { writable } from 'svelte/store';

  interface BanOrder {
    enter_at: number;
    exit_at: number;
    enter_tag: string;
    exit_tag: string;
    short: boolean;
    leverage: number;
    strategy: string;
    enter_create_at: number;
    exit_create_at: number;
    enter_average: number;
    exit_average: number;
    enter_amount: number;
    exit_amount: number;
    enter_cost: number;
    init_price: number;
    profit: number;
    profit_rate: number;
    exit_filled: boolean;
    duration: number;
  }

  interface TradeInfo {
    line_color: string;
    in_color: string;
    in_text: string;
    out_color?: string;
    out_text?: string;
  }

  const kcCtx = writable<ChartCtx>(new ChartCtx());
  let saveRaw = new ChartSave();
  saveRaw.key = 'chart';
  const kcSave = persisted(saveRaw.key, saveRaw);
  const TRADE_GROUP = 'ban_trades';
  let tradeList = $state<BanOrder[]>([]);
  let kc: Chart;

  onMount(() => {
    let pair = $page.url.searchParams.get('pair');
    const initDone = derived(kcCtx, ($c) => $c.initDone)
    initDone.subscribe(() => {
      if(pair){
        kcSave.update((s) => {
          s.symbol.ticker = pair
          s.symbol.shortName = pair
          return s
        })
      }
    })
    const pairChanged = derived(kcSave, ($s) => $s.symbol.ticker);
    pairChanged.subscribe(async (ticker: string) => {
      const rsp = await getAccApi('/orders', { symbols: ticker, source: 'bot' });
      if (rsp.code === 200) {
        tradeList = rsp.data ?? [];
      } else {
        alerts.addAlert('error', rsp.msg ?? m.load_orders_failed());
        return;
      }
      loadVisibleTrades();
    })
  });

  async function loadVisibleTrades() {
    const chartObj = kc.getChart();
    if (!chartObj) return;
    // Remove old trade overlays
    chartObj.removeOverlay({ groupId: TRADE_GROUP });
    
    const dataList = chartObj.getDataList();
    if (!dataList.length) return;

    const startMs = dataList[0].timestamp;
    const stopMs = dataList[dataList.length - 1].timestamp;
    const showTrades = tradeList.filter(td => startMs <= td.enter_at && td.exit_at <= stopMs);
    
    if (!showTrades.length) return;

    const overlays = showTrades.map(td => {
      const color = td.short ? '#FF9600' : '#1677FF';
      const exitColor = td.short ? '#935EBD' : '#01C5C4';
      const inAction = `${td.short ? m.open_short() : m.open_long()}`;
      const outAction = `${td.short ? m.close_short() : m.close_long()}`;

      const inText = `${inAction} ${td.enter_tag} ${td.leverage}${m.times()}
${td.strategy}
${m.order()}: ${fmtDateStr(td.enter_at)}
${m.entry()}: ${fmtDateStr(td.enter_create_at)}
${m.price()}: ${td.enter_average?.toFixed(5)}
${m.amount()}: ${td.enter_amount.toFixed(6)}
${m.cost()}: ${td.enter_cost?.toFixed(2)}`;

      const points = [{
        timestamp: td.enter_create_at,
        value: td.enter_average ?? td.init_price
      }];

      if (td.exit_filled) {
        const outText = `${outAction} ${td.exit_tag} ${td.leverage}${m.times()}
${td.strategy}
${m.order()}: ${fmtDateStr(td.exit_at)}
${m.exit()}: ${fmtDateStr(td.exit_create_at ?? 0)}
${m.price()}: ${td.exit_average?.toFixed(5)}
${m.amount()}: ${td.exit_amount?.toFixed(6)}
${m.profit()}: ${(td.profit_rate * 100).toFixed(1)}% ${td.profit.toFixed(5)}
${m.holding()}: ${fmtDuration(td.duration)}`;

        points.push({
          timestamp: td.exit_create_at ?? 0,
          value: td.exit_average ?? 0
        });

        return {
          name: 'trade',
          groupId: TRADE_GROUP,
          points,
          extendData: {
            line_color: color,
            in_color: color,
            in_text: inText,
            out_color: exitColor,
            out_text: outText
          } as TradeInfo
        } as OverlayCreate;
      }

      return {
        name: 'note',
        groupId: TRADE_GROUP,
        points,
        extendData: {
          line_color: color,
          in_color: color,
          in_text: inText,
        } as TradeInfo
      } as OverlayCreate;
    });

    overlays.forEach(overlay => {
      chartObj.createOverlay(overlay);
    });
  }
</script>

<div class="w-full h-full flex flex-col pb-2">
  <Chart bind:this={kc} ctx={kcCtx} save={kcSave} />
</div>
