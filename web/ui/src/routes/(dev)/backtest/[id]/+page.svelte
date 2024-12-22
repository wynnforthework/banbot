<script lang="ts">
  import { page } from '$app/stores';
  import { onMount } from 'svelte';
  import { getApi } from '@/lib/netio';
  import { alerts } from "$lib/stores/alerts";
  import CodeMirror from '$lib/dev/CodeMirror.svelte';
  import { oneDark } from '@codemirror/theme-one-dark';
  import type { Extension } from '@codemirror/state';
  import * as m from '$lib/paraglide/messages.js';
  import Chart from '$lib/kline/Chart.svelte';
  import { site } from '$lib/stores/site';
  import { showPairs } from '$lib/dev/common';
  import type { BacktestDetail, Order, GroupItem } from '$lib/dev/common';
  import OrderDetail from '$lib/order/OrderDetail.svelte';

  let { id } = $page.params;
  let btPath = $state('');
  let detail = $state<BacktestDetail | null>(null);
  let activeTab = $state('overview');
  let theme: Extension = oneDark;
  let configText = $state('');
  let editor: CodeMirror | null = $state(null);
  let task = $state<any>(null);

  // 订单列表相关状态
  let orders = $state<Order[]>([]);
  let total = $state(0);
  let currentPage = $state(1);
  let pageSize = $state(20);
  let symbol = $state('');
  let strategy = $state('');
  let enterTag = $state('');
  let exitTag = $state('');
  let startMS = $state(0);
  let endMS = $state(0);

  // 日志相关状态
  let logs = $state('');
  let logStart = $state(0);
  let logLimit = $state(1000);
  let loadingLogs = $state(false);
  let assetsUrl = $derived.by(() => {
    return `${$site.apiHost}/api/dev/bt_html?task_id=${id}&type=assets`;
  })
  let entersUrl = $derived.by(() => {
    return `${$site.apiHost}/api/dev/bt_html?task_id=${id}&type=enters`;
  })

  // K线图相关状态
  let selectedOrder = $state<Order | null>(null);
  let timeRange = $state({ start: 0, end: 0 });
  let showOrderModal = $state(false);

  let activeGroupTab = $state('pairs'); // 添加分组tab状态

  onMount(async () => {
    await loadDetail();
  });

  async function loadDetail() {
    const rsp = await getApi('/dev/bt_detail', { task_id: id });
    if(rsp.code != 200) {
      alerts.addAlert("error", rsp.msg || 'load detail failed');
      return;
    }
    btPath = rsp.path;
    detail = rsp.detail;
    task = rsp.task;
  }

  async function loadOrders() {
    const rsp = await getApi('/dev/bt_orders', {
      task_id: id,
      page: currentPage,
      page_size: pageSize,
      symbol,
      strategy,
      enter_tag: enterTag,
      exit_tag: exitTag,
      start_ms: startMS,
      end_ms: endMS
    });
    if(rsp.code != 200) {
      alerts.addAlert("error", rsp.msg || 'load orders failed');
      return;
    }
    orders = rsp.orders;
    total = rsp.total;
  }

  async function loadConfig() {
    const rsp = await getApi('/dev/bt_config', { task_id: id });
    if(rsp.code != 200) {
      alerts.addAlert("error", rsp.msg || 'load config failed');
      return;
    }
    configText = rsp.data;
    if(editor) {
      editor.setValue('config.yml', configText);
    }
  }

  async function loadLogs() {
    if(loadingLogs) return;
    loadingLogs = true;
    try {
      const rsp = await getApi('/dev/bt_logs', {
        task_id: id,
        end: logStart,
        limit: logLimit
      });
      if(rsp.code != 200) {
        alerts.addAlert("error", rsp.msg || 'load logs failed');
        return;
      }
      logs += rsp.data;
      logStart = rsp.start;
    } finally {
      loadingLogs = false;
    }
  }

  $effect(() => {
    if(activeTab === 'orders') {
      loadOrders();
    } else if(activeTab === 'config') {
      loadConfig();
    } else if(activeTab === 'logs' && !logs) {
      loadLogs();
    }
  });

  function formatTime(ms: number) {
    return new Date(ms).toLocaleString();
  }

  function formatNumber(num: number, decimals = 2) {
    if(!num) return '0';
    return num.toFixed(decimals);
  }

  function onOrderClick(order: Order) {
    selectedOrder = order;
    showOrderModal = true;
    timeRange = {
      start: order.enter_at - 24 * 3600 * 1000,
      end: order.exit_at + 24 * 3600 * 1000
    };
  }

  function formatPercent(num: number, decimals = 1) {
    if(!num) return '0%';
    return num.toFixed(decimals) + '%';
  }

  function formatDuration(ms: number) {
    const hours = Math.floor(ms / (3600 * 1000));
    const minutes = Math.floor((ms % (3600 * 1000)) / (60 * 1000));
    if(hours > 0) {
      return `${hours}h${minutes}m`;
    }
    return `${minutes}m`;
  }
</script>

<div class="container mx-auto max-w-[1500px] px-4 py-6">
  <div class="flex justify-between items-center mb-6">
    <h1 class="text-2xl font-bold">{m.bt_report()}: {id}</h1>
    <div class="text-sm opacity-75">{btPath}</div>
  </div>

  <div class="flex gap-4">
    <!-- 左侧导航 -->
    <div class="w-48 flex-shrink-0">
      <ul class="menu bg-base-200 rounded-box">
        <li>
          <button class:active={activeTab === 'overview'} onclick={() => activeTab = 'overview'}>
            {m.bt_overview()}
          </button>
        </li>
        <li>
          <button class:active={activeTab === 'assets'} onclick={() => activeTab = 'assets'}>
            {m.bt_assets()}
          </button>
        </li>
        <li>
          <button class:active={activeTab === 'enters'} onclick={() => activeTab = 'enters'}>
            {m.bt_enters()}
          </button>
        </li>
        <li>
          <button class:active={activeTab === 'config'} onclick={() => activeTab = 'config'}>
            {m.bt_config()}
          </button>
        </li>
        <li>
          <button class:active={activeTab === 'orders'} onclick={() => activeTab = 'orders'}>
            {m.bt_orders()}
          </button>
        </li>
        <li>
          <button class:active={activeTab === 'analysis'} onclick={() => activeTab = 'analysis'}>
            {m.bt_analysis()}
          </button>
        </li>
        <li>
          <button class:active={activeTab === 'logs'} onclick={() => activeTab = 'logs'}>
            {m.bt_logs()}
          </button>
        </li>
      </ul>
    </div>

    <!-- 右侧内容 -->
    <div class="flex-1">
      {#if activeTab === 'overview' && detail}
        <!-- 重要统计信息 -->
        <div class="stats stats-vertical lg:stats-horizontal shadow mb-6">
          <div class="stat">
            <div class="stat-title">{m.init_amount()}</div>
            <div class="stat-value text-success">{formatNumber(task?.walletAmount || 0, 2)}</div>
            <div class="stat-desc">
              {m.leverage()} {task?.leverage}x
              {#if task?.stakeAmount}
                · {m.stake_amt()} {formatNumber(task.stakeAmount, 2)}
              {/if}
            </div>
          </div>

          <div class="stat">
            <div class="stat-title">{m.profit_rate()}</div>
            <div class="stat-value text-success">{formatPercent(detail.totProfitPct, 1)}</div>
            <div class="stat-desc">
              {m.tot_profit()} {formatNumber(detail.totProfit, 2)}
            </div>
          </div>

          <div class="stat">
            <div class="stat-title">{m.win_rate()}</div>
            <div class="stat-value">{formatPercent(detail.winRatePct)}</div>
            <div class="stat-desc">
              {m.order_num()} {detail.orderNum}
            </div>
          </div>

          <div class="stat">
            <div class="stat-title">{m.max_drawdown()}</div>
            <div class="stat-value text-warning">
              {formatPercent(detail.maxDrawDownPct)}
            </div>
            <div class="stat-desc">
              {formatNumber(detail.maxDrawDownVal)} USDT
            </div>
          </div>
          
          <div class="stat">
            <div class="stat-title">{m.show_drawdown()}</div>
            <div class="stat-value text-warning">
              {formatPercent(detail.showDrawDownPct)}
            </div>
            <div class="stat-desc">
              {formatNumber(detail.showDrawDownVal)} USDT
            </div>
          </div>

          <div class="stat">
            <div class="stat-title">{m.sharpe_sortino()}</div>
            <div class="stat-value">{formatNumber(detail.sharpeRatio, 2)}</div>
            <div class="stat-desc">{formatNumber(detail.sortinoRatio, 2)}</div>
          </div>
        </div>

        <!-- 次要统计信息 -->
        <div class="grid grid-cols-2 lg:grid-cols-3 gap-4 mb-6">
          <div class="card bg-base-200">
            <div class="card-body">
              <h3 class="card-title text-sm">{m.bt_range()}</h3>
              <p>{formatTime(detail.startMS)} ~ {formatTime(detail.endMS)}</p>
            </div>
          </div>

          <div class="card bg-base-200">
            <div class="card-body">
              <h3 class="card-title text-sm">{m.total_invest()}/{m.final_balance()}</h3>
              <p>
                {task?.walletAmount ? formatNumber(task.walletAmount) : formatNumber(detail.totalInvest)} / 
                {formatNumber(detail.maxReal)}
              </p>
            </div>
          </div>

          <div class="card bg-base-200">
            <div class="card-body">
              <h3 class="card-title text-sm">{m.tot_fee()}</h3>
              <p>{formatNumber(detail.totFee)} ({formatPercent(detail.totFee/detail.totProfit*100)})</p>
            </div>
          </div>

          <div class="card bg-base-200">
            <div class="card-body">
              <h3 class="card-title text-sm">{m.strategy()}</h3>
              <p class="break-all">{task?.strats || '-'}</p>
            </div>
          </div>

          <div class="card bg-base-200">
            <div class="card-body">
              <h3 class="card-title text-sm">{m.time_period()}/{m.bar_num()}</h3>
              <p>{task?.periods || '-'} / {detail.barNum}</p>
            </div>
          </div>

          <div class="card bg-base-200">
            <div class="card-body">
              <h3 class="card-title text-sm">{m.symbol()}/{m.max_open_orders()}</h3>
              <p class="break-all">{task?.pairs ? showPairs(task.pairs) : '-'} / {detail.maxOpenOrders}</p>
            </div>
          </div>
        </div>

        <!-- 分组统计 -->
        <div class="card bg-base-200 mb-6">
          <div class="card-body">
            <div role="tablist" class="tabs tabs-boxed mb-4">
              <button role="tab" 
                class="tab" 
                class:tab-active={activeGroupTab === 'pairs'}
                onclick={() => activeGroupTab = 'pairs'}>
                {m.stat_by_pairs()}
              </button>
              <button role="tab" 
                class="tab"
                class:tab-active={activeGroupTab === 'dates'}
                onclick={() => activeGroupTab = 'dates'}>
                {m.stat_by_dates()}
              </button>
              <button role="tab" 
                class="tab"
                class:tab-active={activeGroupTab === 'profits'}
                onclick={() => activeGroupTab = 'profits'}>
                {m.stat_by_profits()}
              </button>
              <button role="tab" 
                class="tab"
                class:tab-active={activeGroupTab === 'enters'}
                onclick={() => activeGroupTab = 'enters'}>
                {m.stat_by_enters()}
              </button>
              <button role="tab" 
                class="tab"
                class:tab-active={activeGroupTab === 'exits'}
                onclick={() => activeGroupTab = 'exits'}>
                {m.stat_by_exits()}
              </button>
            </div>

            <!-- 按品种统计 -->
            {#if activeGroupTab === 'pairs' && detail.pairGrps?.length > 0}
              <div class="overflow-x-auto">
                <table class="table">
                  <thead>
                    <tr>
                      <th>{m.symbol()}</th>
                      <th>{m.order_num()}</th>
                      <th>{m.win_rate()}</th>
                      <th>{m.tot_profit()}</th>
                      <th>{m.profit_rate()}</th>
                      <th>{m.avg_hold_time()}</th>
                      <th>{m.sharpe_ratio()}/{m.sortino_ratio()}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#each detail.pairGrps as group}
                      <tr>
                        <td>{group.title}</td>
                        <td>{group.Orders?.length || 0}</td>
                        <td>{formatPercent(group.WinCount * 100 / (group.Orders?.length || 1))}</td>
                        <td>{formatNumber(group.profitSum)}</td>
                        <td>{formatPercent(group.profitSum * 100 / group.costSum)}</td>
                        <td>{formatDuration(group.durations.reduce((a: number, b: number) => a + b, 0) * 1000 / group.durations.length)}</td>
                        <td>{formatNumber(group.sharpe, 2)} / {formatNumber(group.sortino, 2)}</td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}

            <!-- 按日期统计 -->
            {#if activeGroupTab === 'dates' && detail.dateGrps?.length > 0}
              <div class="overflow-x-auto">
                <table class="table">
                  <thead>
                    <tr>
                      <th>{m.date()}</th>
                      <th>{m.order_num()}</th>
                      <th>{m.win_rate()}</th>
                      <th>{m.tot_profit()}</th>
                      <th>{m.profit_rate()}</th>
                      <th>{m.avg_hold_time()}</th>
                      <th>{m.enter_tag()}</th>
                      <th>{m.exit_tag()}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#each detail.dateGrps as group}
                      <tr>
                        <td>{group.title}</td>
                        <td>{group.Orders?.length || 0}</td>
                        <td>{formatPercent(group.WinCount * 100 / (group.Orders?.length || 1))}</td>
                        <td>{formatNumber(group.profitSum)}</td>
                        <td>{formatPercent(group.profitSum * 100 / group.costSum)}</td>
                        <td>{formatDuration(group.durations.reduce((a: number, b: number) => a + b, 0) * 1000 / group.durations.length)}</td>
                        <td class="whitespace-normal max-w-[200px]">
                          {group.Orders?.map((o: Order) => o.enter_tag)
                            .filter((v: string, i: number, a: string[]) => a.indexOf(v) === i)
                            .join(', ')}
                        </td>
                        <td class="whitespace-normal max-w-[200px]">
                          {group.Orders?.map((o: Order) => o.exit_tag)
                            .filter((v: string, i: number, a: string[]) => a.indexOf(v) === i)
                            .join(', ')}
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}

            <!-- 按收益区间统计 -->
            {#if activeGroupTab === 'profits' && detail.profitGrps?.length > 0}
              <div class="overflow-x-auto">
                <table class="table">
                  <thead>
                    <tr>
                      <th>{m.profit_range()}</th>
                      <th>{m.order_num()}</th>
                      <th>{m.win_rate()}</th>
                      <th>{m.tot_profit()}</th>
                      <th>{m.profit_rate()}</th>
                      <th>{m.avg_hold_time()}</th>
                      <th>{m.enter_tag()}</th>
                      <th>{m.exit_tag()}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#each detail.profitGrps as group}
                      <tr>
                        <td>{group.title}</td>
                        <td>{group.Orders?.length || 0}</td>
                        <td>{formatPercent(group.WinCount * 100 / (group.Orders?.length || 1))}</td>
                        <td>{formatNumber(group.profitSum)}</td>
                        <td>{formatPercent(group.profitSum * 100 / group.costSum)}</td>
                        <td>{formatDuration(group.durations.reduce((a: number, b: number) => a + b, 0) * 1000 / group.durations.length)}</td>
                        <td class="whitespace-normal max-w-[200px]">
                          {group.Orders?.map((o: Order) => o.enter_tag)
                            .filter((v: string, i: number, a: string[]) => a.indexOf(v) === i)
                            .join(', ')}
                        </td>
                        <td class="whitespace-normal max-w-[200px]">
                          {group.Orders?.map((o: Order) => o.exit_tag)
                            .filter((v: string, i: number, a: string[]) => a.indexOf(v) === i)
                            .join(', ')}
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}

            <!-- 按入场信号统计 -->
            {#if activeGroupTab === 'enters' && detail.enterGrps?.length > 0}
              <div class="overflow-x-auto">
                <table class="table">
                  <thead>
                    <tr>
                      <th>{m.enter_tag()}</th>
                      <th>{m.order_num()}</th>
                      <th>{m.win_rate()}</th>
                      <th>{m.tot_profit()}</th>
                      <th>{m.profit_rate()}</th>
                      <th>{m.avg_hold_time()}</th>
                      <th>{m.sharpe_ratio()}/{m.sortino_ratio()}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#each detail.enterGrps as group}
                      <tr>
                        <td>{group.title}</td>
                        <td>{group.Orders?.length || 0}</td>
                        <td>{formatPercent(group.WinCount * 100 / (group.Orders?.length || 1))}</td>
                        <td>{formatNumber(group.profitSum)}</td>
                        <td>{formatPercent(group.profitSum * 100 / group.costSum)}</td>
                        <td>{formatDuration(group.durations.reduce((a: number, b: number) => a + b, 0) * 1000 / group.durations.length)}</td>
                        <td>{formatNumber(group.sharpe, 2)} / {formatNumber(group.sortino, 2)}</td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}

            <!-- 按出场信号统计 -->
            {#if activeGroupTab === 'exits' && detail.exitGrps?.length > 0}
              <div class="overflow-x-auto">
                <table class="table">
                  <thead>
                    <tr>
                      <th>{m.exit_tag()}</th>
                      <th>{m.order_num()}</th>
                      <th>{m.win_rate()}</th>
                      <th>{m.tot_profit()}</th>
                      <th>{m.profit_rate()}</th>
                      <th>{m.avg_hold_time()}</th>
                      <th>{m.sharpe_ratio()}/{m.sortino_ratio()}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {#each detail.exitGrps as group}
                      <tr>
                        <td>{group.title}</td>
                        <td>{group.Orders?.length || 0}</td>
                        <td>{formatPercent(group.WinCount * 100 / (group.Orders?.length || 1))}</td>
                        <td>{formatNumber(group.profitSum)}</td>
                        <td>{formatPercent(group.profitSum * 100 / group.costSum)}</td>
                        <td>{formatDuration(group.durations.reduce((a: number, b: number) => a + b, 0) * 1000 / group.durations.length)}</td>
                        <td>{formatNumber(group.sharpe, 2)} / {formatNumber(group.sortino, 2)}</td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
            {/if}

          </div>
        </div>

      {:else if activeTab === 'assets'}
        <iframe src={assetsUrl} class="w-full h-[800px]" title="资产净值曲线" ></iframe>

      {:else if activeTab === 'enters'}
        <iframe src={entersUrl} class="w-full h-[800px]" title="入场信号曲线" ></iframe>

      {:else if activeTab === 'config'}
        <CodeMirror bind:this={editor} {theme} readonly={true} class="h-[800px]" />

      {:else if activeTab === 'orders'}
        <div class="flex flex-col gap-4">
          <!-- 过滤器 -->
          <div class="flex flex-wrap gap-4">
            <input type="text" placeholder="交易对" class="input input-bordered w-32"
              bind:value={symbol} onchange={loadOrders}
            />
            <input type="text" placeholder="策略" class="input input-bordered w-32"
              bind:value={strategy} onchange={loadOrders}
            />
            <input type="text" placeholder="入场标签" class="input input-bordered w-32"
              bind:value={enterTag} onchange={loadOrders}
            />
            <input type="text" placeholder="出场标签" class="input input-bordered w-32"
              bind:value={exitTag} onchange={loadOrders}
            />
          </div>

          <!-- 订单表格 -->
          <div class="overflow-x-auto">
            <table class="table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>{m.symbol()}</th>
                  <th>{m.direction()}</th>
                  <th>{m.leverage()}</th>
                  <th>{m.enter_at()}</th>
                  <th>{m.enter_tag()}</th>
                  <th>{m.enter_price()}</th>
                  <th>{m.enter_amount()}</th>
                  <th>{m.exit_at()}</th>
                  <th>{m.exit_tag()}</th>
                  <th>{m.exit_price()}</th>
                  <th>{m.exit_amount()}</th>
                  <th>{m.profit()}</th>
                  <th>{m.profit_rate()}</th>
                </tr>
              </thead>
              <tbody>
                {#each orders as order}
                  <tr class="hover:bg-base-300 cursor-pointer" onclick={() => onOrderClick(order)}>
                    <td>{order.id}</td>
                    <td>{order.symbol}</td>
                    <td>{order.short ? '做空' : '做多'}</td>
                    <td>{order.leverage}x</td>
                    <td>{formatTime(order.enter_at)}</td>
                    <td>{order.enter_tag}</td>
                    <td>{formatNumber(order.enter_price || 0, 8)}</td>
                    <td>{formatNumber(order.enter_amount || 0, 8)}</td>
                    <td>{formatTime(order.exit_at)}</td>
                    <td>{order.exit_tag}</td>
                    <td>{formatNumber(order.exit_price || 0, 8)}</td>
                    <td>{formatNumber(order.exit_amount || 0, 8)}</td>
                    <td>{formatNumber(order.profit)}</td>
                    <td>{formatNumber(order.profit_rate * 100)}%</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>

          <!-- 分页 -->
          <div class="flex justify-between items-center">
            <div class="flex items-center gap-2">
              <span>每页显示:</span>
              <select class="select select-bordered w-20" bind:value={pageSize} onchange={loadOrders}>
                <option value={10}>10</option>
                <option value={20}>20</option>
                <option value={50}>50</option>
                <option value={100}>100</option>
              </select>
            </div>
            <div class="join">
              <button class="join-item btn" disabled={currentPage === 1}
                onclick={() => { currentPage--; loadOrders(); }}>
                {m.prev_page()}
              </button>
              <button class="join-item btn btn-disabled">
                第 {currentPage} 页 / 共 {Math.ceil(total / pageSize)} 页
              </button>
              <button class="join-item btn" disabled={currentPage >= Math.ceil(total / pageSize)}
                onclick={() => { currentPage++; loadOrders(); }}>
                {m.next_page()}
              </button>
            </div>
          </div>
        </div>

      {:else if activeTab === 'analysis'}
        <div class="flex flex-col gap-4">
          <!-- K线图区域 -->
          <div class="h-[600px] bg-base-200 rounded-box">
            {#if selectedOrder}
              <Chart
                pair={selectedOrder.Symbol}
                startMS={timeRange.start}
                endMS={timeRange.end}
                orders={[selectedOrder]}
              />
            {:else}
              <div class="flex items-center justify-center h-full text-lg opacity-50">
                请从订单列表中选择一个订单查看详情
              </div>
            {/if}
          </div>

          <!-- 时间范围选择器 -->
          {#if selectedOrder}
            <div class="flex items-center gap-4">
              <span>{m.time_range()}:</span>
              <input type="datetime-local" class="input input-bordered"
                value={new Date(timeRange.start).toISOString().slice(0, 16)}
                onchange={(e) => timeRange.start = new Date(e.currentTarget.value).getTime()}
              />
              <span>至</span>
              <input type="datetime-local" class="input input-bordered"
                value={new Date(timeRange.end).toISOString().slice(0, 16)}
                onchange={(e) => timeRange.end = new Date(e.currentTarget.value).getTime()}
              />
            </div>
          {/if}

          <!-- 订单列表 -->
          <div class="overflow-x-auto">
            <table class="table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>{m.symbol()}</th>
                  <th>{m.direction()}</th>
                  <th>{m.enter_at()}</th>
                  <th>{m.enter_tag()}</th>
                  <th>{m.enter_price()}</th>
                  <th>{m.exit_at()}</th>
                  <th>{m.exit_tag()}</th>
                  <th>{m.exit_price()}</th>
                  <th>{m.profit()}</th>
                  <th>{m.profit_rate()}</th>
                </tr>
              </thead>
              <tbody>
                {#each orders.filter(o => 
                  o.enter_at >= timeRange.start && o.exit_at <= timeRange.end
                ) as order}
                  <tr class="hover:bg-base-300 cursor-pointer"
                    class:bg-primary={selectedOrder?.id === order.id}
                    onclick={() => onOrderClick(order)}
                  >
                    <td>{order.id}</td>
                    <td>{order.symbol}</td>
                    <td>{order.short ? '做空' : '做多'}</td>
                    <td>{formatTime(order.enter_at)}</td>
                    <td>{order.enter_tag}</td>
                    <td>{formatNumber(order.enter_price || 0, 8)}</td>
                    <td>{formatTime(order.exit_at)}</td>
                    <td>{order.exit_tag}</td>
                    <td>{formatNumber(order.exit_price || 0, 8)}</td>
                    <td>{formatNumber(order.profit)}</td>
                    <td>{formatNumber(order.profit_rate * 100)}%</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div>

      {:else if activeTab === 'logs'}
        <div class="flex flex-col gap-4">
          <div class="bg-base-200 rounded-box p-4 font-mono text-sm whitespace-pre-wrap h-[800px] overflow-y-auto">
            {logs}
          </div>
          {#if logStart > 0}
            <button class="btn btn-outline w-full" disabled={loadingLogs} onclick={loadLogs}>
              {loadingLogs ? '加载中...' : '加载更多'}
            </button>
          {/if}
        </div>
      {/if}
    </div>
  </div>

  <!-- 订单详情弹窗 -->
  {#if showOrderModal && selectedOrder}
    <div class="modal modal-open">
      <div class="modal-box max-w-3xl">
        <h3 class="font-bold text-lg mb-4">{m.order_detail()}</h3>
        <OrderDetail order={selectedOrder} editable={false} />
        <div class="modal-action">
          <button class="btn" onclick={() => showOrderModal = false}>{m.close()}</button>
        </div>
      </div>
    </div>
  {/if}
</div>