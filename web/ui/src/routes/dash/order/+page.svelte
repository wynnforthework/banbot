<script lang="ts">
  import { onMount } from 'svelte';
  import * as m from '$lib/paraglide/messages';
  import { getAccApi, postAccApi } from '$lib/netio';
  import { alerts } from '$lib/stores/alerts';
  import {modals} from '$lib/stores/modals';
  import Modal from '$lib/kline/Modal.svelte';
  import { fmtDateStr, toUTCStamp, curTZ } from '$lib/dateutil';
  import { OrderDetail, type InOutOrder } from '$lib/order';
  import {getFirstValid} from "$lib/common";
  import { pagination } from '@/lib/snippets.svelte';

  let tabName = $state('bot'); // bot/exchange/position
  let banodList = $state<InOutOrder[]>([]);
  let exgodList = $state<any[]>([]);
  let exgposList = $state<any[]>([]);
  let pageSize = $state(15);
  let limitSize = $state(30);
  let currentPage = $state(1);
  let showOdDetail = $state(false);
  let showExOdDetail = $state(false);
  let showOpenOrder = $state(false);
  let showCloseExg = $state(false);
  let openingOd = $state(false);
  let closingExgPos = $state(false);
  let exFilter = $state('');
  let selectedOrder = $state<InOutOrder|null>(null);
  let pageFirst = $state(0);
  let pageLast = $state(0);

  let search = $state({
    status: '',
    symbols: '',
    startMs: '',
    stopMs: ''
  });

  let openOd = $state({
    pair: '',
    side: 'long',
    orderType: 'market',
    price: undefined,
    stopLossPrice: undefined,
    enterCost: undefined,
    enterTag: undefined,
    leverage: undefined,
    strategy: undefined
  });

  let closeExgPos = $state({
    symbol: '',
    side: '',
    initAmount: 0,
    amount: 0,
    percent: 100,
    orderType: 'market',
    price: undefined
  });

  function getQuoteCode(symbol: string) {
    const arr = symbol.split('/');
    if (arr.length !== 2) return '';
    let quote = arr[1];
    quote = quote.split('.')[0];
    return quote.split(':')[0];
  }

  async function loadData(page: number) {
    let afterDirt = -1;
    if (currentPage > page){
      afterDirt = 1;
    }
    currentPage = page;
    const data: Record<string, any> = {...search};
    data.startMs = toUTCStamp(data.startMs);
    data.stopMs = toUTCStamp(data.stopMs);
    data.source = tabName;
    
    if (tabName === 'bot') {
      data.limit = pageSize;
      if (page > 1) {
        if(afterDirt > 0){
          data.afterId = pageFirst * afterDirt;
        }else{
          data.afterId = pageLast * afterDirt;
        }
      }
    } else if (tabName === 'exchange') {
      // exchange和position必须传symbols
      if (!data.symbols) {
        alerts.addAlert('error', m.symbol_required());
        return;
      }
      data.limit = limitSize;
    }

    const rsp = await getAccApi('/orders', data);
    let res = rsp.data ?? [];
    exFilter = '';

    if (tabName === 'bot') {
      banodList = res;
      if (res.length > 0) {
        pageLast = res[res.length - 1].id;
        pageFirst = res[0].id;
      }
    } else if (tabName === 'exchange') {
      if (search.status) {
        const oldNum = res.length;
        res = res.filter((od: any) => od.filled);
        exFilter = `已筛选：${res.length}/${oldNum}`;
      }
      exgodList = res;
    } else {
      exgposList = res;
    }
  }

  async function closeOrder(orderId: string) {
    if (!await modals.confirm(m.confirm_close_position())) return;
    
    const rsp = await postAccApi('/forceexit', {orderId});
    if(rsp.code !== 200){
      console.error('close order failed', rsp);
      alerts.addAlert('error', rsp.msg ?? m.close_order_failed());
      return;
    }
    
    const closeNum = rsp.closeNum ?? 0;
    const failNum = rsp.failNum ?? 0;
    
    let message = m.closed_positions({close: closeNum, fail: failNum});
    if (rsp.errMsg) {
      message += `\n${rsp.errMsg}`;
    }
    alerts.addAlert(failNum > 0 ? 'warning' : 'success', message);
    await loadData(1);
  }

  function onPosAmountChg(percent: number) {
    closeExgPos.amount = parseFloat((closeExgPos.initAmount * percent / 100).toFixed(5));
  }

  function showOrder(idx: number) {
    if (tabName === 'bot') {
      selectedOrder = banodList[idx];
      showOdDetail = true;
    } else if (tabName === 'exchange') {
      selectedOrder = exgodList[idx];
      showExOdDetail = true;
    } else if (tabName === 'position') {
      selectedOrder = exgposList[idx];
      showExOdDetail = true;
    }
  }

  function clickCloseExgPos(index: number) {
    const item = exgposList[index];
    closeExgPos = {
      symbol: item.symbol,
      amount: item.contracts,
      side: item.side,
      initAmount: item.contracts,
      percent: 100,
      orderType: 'market',
      price: undefined
    };
    showCloseExg = true;
  }

  async function closeExgPosition(all: boolean = false) {
    let data = {...closeExgPos};
    if (all) {
      if (!await modals.confirm(m.confirm_close_all_positions())) return;
      data.symbol = 'all';
    }

    const rsp = await postAccApi('/close_pos', data);

    const closeNum = rsp.close_num ?? 0;
    const doneNum = rsp.done_num ?? 0;
    let message = m.closed_orders({num: closeNum});
    if (doneNum) {
      message += m.filled_orders({num: doneNum});
    }
    showCloseExg = false;
    alerts.addAlert('success', message);
    await loadData(1);
  }

  async function doOpenOrder() {
    openingOd = true;
    const rsp = await postAccApi('/forceenter', openOd);
    openingOd = false;
    
    if (rsp.code !== 200) {
      alerts.addAlert('error', rsp.msg ?? m.open_order_failed());
    } else {
      alerts.addAlert('success', m.open_order_success());
      showOpenOrder = false;
    }
  }

  async function clickCalcProfits() {
    const rsp = await postAccApi('/calc_profits', {});
    
    if (rsp.code !== 200) {
      alerts.addAlert('error', m.update_failed({msg: rsp.msg ?? ''}));
    } else {
      alerts.addAlert('success', m.updated_orders({num: rsp.num}));
      await loadData(currentPage);
    }
  }

  onMount(() => {
    loadData(1);
  });
</script>

<!-- 主体内容 -->
<div class="bg-base-100 p-4 min-h-[600px] border border-base-200">
  <div class="flex flex-col gap-4">
    <!-- Tab Menu -->
    <div class="flex justify-between items-center">
      <div class="tabs tabs-boxed bg-base-200/50 p-1 rounded-md">
        <button 
          class="tab tab-sm transition-all duration-200 px-4 {tabName === 'bot' ? 'tab-active bg-primary/90 text-primary-content' : 'hover:bg-base-300'}"
          onclick={() => tabName = 'bot'}
        >
          {m.bot_orders()}
        </button>
        <button 
          class="tab tab-sm transition-all duration-200 px-4 {tabName === 'exchange' ? 'tab-active bg-primary/90 text-primary-content' : 'hover:bg-base-300'}"
          onclick={() => tabName = 'exchange'}
        >
          {m.exchange_orders()}
        </button>
        <button 
          class="tab tab-sm transition-all duration-200 px-4 {tabName === 'position' ? 'tab-active bg-primary/90 text-primary-content' : 'hover:bg-base-300'}"
          onclick={() => tabName = 'position'}
        >
          {m.exchange_positions()}
        </button>
      </div>

      <div class="flex gap-2 items-center">
        <!-- 添加每页大小控制 -->
        <select 
          class="select select-sm select-bordered focus:outline-none w-36 text-sm"
          bind:value={pageSize}
          onchange={() => loadData(1)}
        >
          <option value={10}>10 / {m.page()}</option>
          <option value={15}>15 / {m.page()}</option>
          <option value={20}>20 / {m.page()}</option>
          <option value={30}>30 / {m.page()}</option>
          <option value={50}>50 / {m.page()}</option>
        </select>

        <!-- 现有的操作按钮 -->
        {#if tabName === 'bot'}
          <button class="btn btn-sm bg-primary/90 hover:bg-primary text-primary-content border-none" onclick={clickCalcProfits}>
            {m.update_profits()}
          </button>
          <button class="btn btn-sm bg-primary/90 hover:bg-primary text-primary-content border-none" onclick={() => showOpenOrder = true}>
            {m.open_order()}
          </button>
          <button class="btn btn-sm bg-error/90 hover:bg-error text-error-content border-none" onclick={() => closeOrder('all')}>
            {m.close_all()}
          </button>
        {:else if tabName === 'position'}
          <button class="btn btn-sm bg-primary/90 hover:bg-primary text-primary-content border-none" onclick={() => loadData(1)}>
            {m.refresh()}
          </button>
          <button class="btn btn-sm bg-error/90 hover:bg-error text-error-content border-none" onclick={() => closeExgPosition(true)}>
            {m.close_all()}
          </button>
        {/if}
      </div>
    </div>

    <!-- Search Form -->
    {#if tabName !== 'position'}
      <div class="form-control">
        <div class="flex flex-wrap gap-4">
          {#if tabName === 'bot'}
            <select class="select select-sm select-bordered focus:outline-none text-sm" bind:value={search.status}>
              <option value="">{m.all()}</option>
              <option value="open">{m.pos_opened()}</option>
              <option value="his">{m.closed()}</option>
            </select>
          {/if}

          <input type="text" class="input input-sm input-bordered focus:outline-none text-sm font-mono" placeholder={m.symbol()} bind:value={search.symbols} 
            required={tabName !== 'bot'} />

          <input type="text" class="input input-sm input-bordered focus:outline-none text-sm font-mono" placeholder="20231012" bind:value={search.startMs} />

          {#if tabName === 'bot'}
            <input type="text" class="input input-sm input-bordered focus:outline-none text-sm font-mono" placeholder="20231012" bind:value={search.stopMs} />
          {:else}
            <input type="number" class="input input-sm input-bordered focus:outline-none text-sm font-mono" bind:value={limitSize} />
          {/if}

          <button 
            class="btn btn-sm bg-primary/90 hover:bg-primary text-primary-content border-none" 
            onclick={() => {
              if (tabName !== 'bot' && !search.symbols) {
                alerts.addAlert('error', m.symbol_required());
                return;
              }
              loadData(1);
            }}
          >
            {m.search()}
          </button>

          {#if exFilter}
            <span class="text-info">{exFilter}</span>
          {/if}
        </div>
      </div>
    {/if}

    <!-- Data Tables -->
    <div class="overflow-x-auto">
      {#if tabName === 'bot'}
        <table class="table table-sm">
          <thead>
            <tr>
              <th class="bg-base-200/30 font-medium text-sm">ID</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.pair()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.timeframe()}/{m.side()}/{m.leverage()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.enter_time()}({curTZ()})</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.enter_tag()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.enter_price()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.exit_time()}({curTZ()})</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.exit_tag()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.profit_rate()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.profit()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.actions()}</th>
            </tr>
          </thead>
          <tbody>
            {#each banodList as order, i}
              <tr class="hover:bg-base-200/50 transition-colors">
                <td class="text-sm font-mono">{order.id}</td>
                <td class="text-sm font-mono">{order.symbol}</td>
                <td>{order.timeframe}/{order.short ? m.short() : m.long()}/{order.leverage}</td>
                <td class="text-sm">{fmtDateStr(order.enter_at)}</td>
                <td>{order.enter_tag}</td>
                <td>{getFirstValid([order.enter?.average, order.enter?.price, order.init_price]).toFixed(7)}</td>
                <td class="text-sm">{fmtDateStr(order.exit_at)}</td>
                <td>{order.exit_tag}</td>
                <td>{(order.profit_rate * 100).toFixed(1)}%</td>
                <td>{order.profit.toFixed(5)}</td>
                <td>
                  <div class="flex gap-2">
                    <button class="btn btn-xs bg-primary/90 hover:bg-primary text-primary-content border-none" onclick={() => showOrder(i)}>
                      {m.view()}
                    </button>
                    {#if order.status !== 4}
                      <button class="btn btn-xs bg-error/90 hover:bg-error text-error-content border-none" onclick={() => closeOrder(order.id.toString())}>
                        {m.close_order()}
                      </button>
                    {/if}
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else if tabName === 'exchange'}
        <table class="table table-sm">
          <thead>
            <tr>
              <th class="bg-base-200/30 font-medium text-sm">{m.order_id()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.client_id()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.pair()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.time()}({curTZ()})</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.side()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.position_type()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.price()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.amount()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.status()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.actions()}</th>
            </tr>
          </thead>
          <tbody>
            {#each exgodList as order, i}
              <tr class="hover:bg-base-200/50 transition-colors">
                <td>{order.id}</td>
                <td>{order.clientOrderId}</td>
                <td>{order.symbol}</td>
                <td>{fmtDateStr(order.timestamp)}</td>
                <td>{order.side.toLowerCase() === 'buy' ? m.long() : m.short()}</td>
                <td>{order.reduceOnly ? m.close_position() : m.open_position()}</td>
                <td>{order.average || order.price || '-'}</td>
                <td>{order.amount}</td>
                <td>{order.status}</td>
                <td>
                  <button class="btn btn-xs bg-primary/90 hover:bg-primary text-primary-content border-none" onclick={() => showOrder(i)}>
                    {m.view()}
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <table class="table table-sm">
          <thead>
            <tr>
              <th class="bg-base-200/30 font-medium text-sm">{m.pair()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.time()}({curTZ()})</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.side()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.entry_price()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.mark_price()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.notional()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.leverage()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.unrealized_pnl()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.margin_mode()}</th>
              <th class="bg-base-200/30 font-medium text-sm">{m.actions()}</th>
            </tr>
          </thead>
          <tbody>
            {#each exgposList as pos, i}
              <tr class="hover:bg-base-200/50 transition-colors">
                <td>{pos.symbol}</td>
                <td>{fmtDateStr(pos.timestamp)}</td>
                <td>{pos.side === 'long' ? m.long() : m.short()}</td>
                <td>{pos.entryPrice?.toFixed(4)}</td>
                <td>{pos.markPrice?.toFixed(4)}</td>
                <td>{pos.notional?.toFixed(4)}</td>
                <td>{pos.leverage}x</td>
                <td class={pos.unrealizedPnl >= 0 ? 'text-success' : 'text-error'}>
                  {pos.unrealizedPnl?.toFixed(4)}
                </td>
                <td>{pos.marginMode}</td>
                <td>
                  <div class="flex gap-2">
                    <button class="btn btn-xs bg-primary/90 hover:bg-primary text-primary-content border-none" onclick={() => showOrder(i)}>
                      {m.view()}
                    </button>
                    <button class="btn btn-xs bg-error/90 hover:bg-error text-error-content border-none" onclick={() => clickCloseExgPos(i)}>
                      {m.close_position()}
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>

    <!-- 修改分页控制 -->
    {#if tabName === 'bot'}
      {@render pagination(0, pageSize, currentPage, loadData, pSize => {pageSize = pSize })}
    {/if}
  </div>
</div>

<!-- Modals -->
<OrderDetail bind:show={showOdDetail} order={selectedOrder} editable={(selectedOrder?.status ?? 0) < 4 } />

<Modal title={m.exchange_details()} bind:show={showExOdDetail} width={800}>
  {#if selectedOrder}
    <pre class="whitespace-pre-wrap">{JSON.stringify(selectedOrder, null, 2)}</pre>
  {/if}
</Modal>

<Modal title={m.open_order()} bind:show={showOpenOrder} width={800} buttons={[]}>
  <div class="space-y-4 p-4">
    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.pair()}</label>
        <input type="text" class="input input-sm input-bordered focus:outline-none text-sm font-mono" placeholder={m.enter_symbol_placeholder()} bind:value={openOd.pair} />
      </div>
    </div>

    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.side()}</label>
        <div class="flex gap-4 flex-1">
          <label class="label cursor-pointer">
            <input type="radio" class="radio" bind:group={openOd.side} value="long" />
            <span class="label-text ml-2">{m.long()}</span>
          </label>
          <label class="label cursor-pointer">
            <input type="radio" class="radio" bind:group={openOd.side} value="short" />
            <span class="label-text ml-2">{m.short()}</span>
          </label>
        </div>
      </div>
    </div>

    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.order_type()}</label>
        <div class="flex gap-4 flex-1">
          <label class="label cursor-pointer">
            <input type="radio" class="radio" bind:group={openOd.orderType} value="market" />
            <span class="label-text ml-2">{m.market_order()}</span>
          </label>
          <label class="label cursor-pointer">
            <input type="radio" class="radio" bind:group={openOd.orderType} value="limit" />
            <span class="label-text ml-2">{m.limit_order()}</span>
          </label>
        </div>
      </div>
    </div>

    {#if openOd.orderType === 'limit'}
      <div class="form-control">
        <div class="flex items-center gap-4">
          <label class="label w-24 whitespace-nowrap">{m.price()}</label>
          <input type="number" class="input input-sm input-bordered focus:outline-none text-sm font-mono" bind:value={openOd.price} step="0.00000001" />
        </div>
      </div>

      <div class="form-control">
        <div class="flex items-center gap-4">
          <label class="label w-24 whitespace-nowrap">{m.stop_loss_price()}</label>
          <input type="number" class="input input-sm input-bordered focus:outline-none text-sm font-mono" bind:value={openOd.stopLossPrice} step="0.00000001" />
        </div>
      </div>
    {/if}

    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.enter_cost()}</label>
        <div class="input-group">
          <input type="number" class="input input-sm input-bordered focus:outline-none text-sm font-mono" bind:value={openOd.enterCost} step="0.00000001" />
          <span class="input-group-addon">{getQuoteCode(openOd.pair)}</span>
        </div>
      </div>
    </div>

    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.enter_tag()}</label>
        <input type="text" class="input input-sm input-bordered focus:outline-none text-sm font-mono" bind:value={openOd.enterTag} />
      </div>
    </div>

    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.leverage()}</label>
        <input type="number" class="input input-sm input-bordered focus:outline-none text-sm font-mono" bind:value={openOd.leverage} min="1" max="200" step="1" />
      </div>
    </div>

    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.strategy()}</label>
        <input type="text" class="input input-sm input-bordered focus:outline-none text-sm font-mono" placeholder={m.enter_strategy_placeholder()} bind:value={openOd.strategy} />
      </div>
    </div>

    <div class="flex justify-end mt-4">
      <button class="btn btn-sm bg-primary/90 hover:bg-primary text-primary-content border-none" disabled={!openOd.pair || !openOd.enterCost} onclick={doOpenOrder}>
        {#if openingOd}
          <span class="loading loading-spinner"></span>
        {/if}
        {m.save()}
      </button>
    </div>
  </div>
</Modal>

<Modal title={m.close_position()} bind:show={showCloseExg} width={800} buttons={[]}>
  <div class="space-y-4 p-4">
    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.pair()}</label>
        <input type="text" class="input input-sm input-bordered focus:outline-none text-sm font-mono" bind:value={closeExgPos.symbol} disabled />
      </div>
    </div>

    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.close_amount()}</label>
        <input type="number" class="input input-sm input-bordered focus:outline-none text-sm font-mono" bind:value={closeExgPos.amount} step="0.00000001" />
      </div>
    </div>

    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{closeExgPos.percent}%</label>
        <input type="range" min="0" max="100" class="range range-primary range-sm" bind:value={closeExgPos.percent} 
          onchange={() => onPosAmountChg(closeExgPos.percent)} />
      </div>
    </div>

    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.order_type()}</label>
        <div class="flex gap-4 flex-1">
          <label class="label cursor-pointer">
            <input type="radio" class="radio" bind:group={closeExgPos.orderType} value="market" />
            <span class="label-text ml-2">{m.market_order()}</span>
          </label>
          <label class="label cursor-pointer">
            <input type="radio" class="radio" bind:group={closeExgPos.orderType} value="limit" />
            <span class="label-text ml-2">{m.limit_order()}</span>
          </label>
        </div>
      </div>
    </div>

    {#if closeExgPos.orderType === 'limit'}
      <div class="form-control">
        <div class="flex items-center gap-4">
          <label class="label w-24 whitespace-nowrap">{m.price()}</label>
          <input type="number" class="input input-sm input-bordered focus:outline-none text-sm font-mono" bind:value={closeExgPos.price} step="0.00000001" />
        </div>
      </div>
    {/if}

    <div class="flex justify-end mt-4">
      <button 
        class="btn btn-sm bg-primary/90 hover:bg-primary text-primary-content border-none" 
        disabled={!closeExgPos.symbol || !closeExgPos.amount}
        onclick={() => closeExgPosition(false)}
      >
        {#if closingExgPos}
          <span class="loading loading-spinner"></span>
        {/if}
        {m.save()}
      </button>
    </div>
  </div>
</Modal>
