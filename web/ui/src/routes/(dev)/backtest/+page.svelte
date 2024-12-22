<script lang="ts">
  import { onMount } from 'svelte';
  import { getApi } from '$lib/netio';
  import {alerts} from '$lib/stores/alerts';
  import type {BtTask} from "$lib/dev/types"
  import * as m from '$lib/paraglide/messages.js'
  import { goto } from '$app/navigation';
  import { showPairs } from '$lib/dev/common';

  let tasks = $state<BtTask[]>([]);
  let strats = $state<string[]>([]);
  let periods = $state<string[]>([]);
  let ranges = $state<{label: string, value: number}[]>([]);
  let hoveredCard = $state<number|null>(null);

  let selectedStrat = $state('');
  let selectedPeriod = $state('');
  let selectedRange = $state<{label: string, value: number}|null>(null);
  let hasPrev = $state(false);
  let hasNext = $state(true);

  onMount(async () => {
    await loadOptions();
  });

  async function loadOptions() {
    const rsp = await getApi('/dev/bt_options');
    if(rsp.code != 200) {
      console.error('load options failed', rsp);
      alerts.addAlert('error', rsp.msg || 'load options failed');
      return;
    }
    strats = rsp.strats || [];
    periods = rsp.periods || [];
    ranges = rsp.ranges || [];
  }

  async function loadPage(next: boolean) {
    console.log('fetch tasks on load page', next);
    if(next) {
      await fetchTasks(tasks[tasks.length - 1].id);
    } else {
      await fetchTasks();
    }
  }

  async function fetchTasks(maxId?: number) {
    const rsp = await getApi('/dev/bt_tasks', {
      mode: 'backtest',
      strat: selectedStrat,
      period: selectedPeriod,
      range: selectedRange,
      limit: 20,
      maxId: maxId || 0
    });
    if(rsp.code != 200) {
      console.error('load backtest tasks failed', rsp);
      alerts.addAlert('error', rsp.msg || 'load backtest tasks failed');
      return;
    }
    tasks = rsp.data;
    hasPrev = !!(maxId && maxId > 0);
    hasNext = tasks.length >= 20;
  }

  $effect(() => {
    // 这行不能删除，用于监控变化
    console.log('filter changed', selectedStrat, selectedPeriod, selectedRange);
    fetchTasks();
  });

  function clickTask(id: number) {
    goto(`/backtest/${id}`);
  }
</script>

<style>
  .custom-tooltip {
    position: absolute;
    background: #333;
    color: white;
    padding: 8px;
    border-radius: 4px;
    font-size: 14px;
    z-index: 1000;
    white-space: pre-line;
    pointer-events: none;
    max-width: 300px;
    top: 100%;
    left: 50%;
    transform: translateX(-50%);
  }
  .loc-top {
    top: auto;
    bottom: 100%;
  }
</style>

<div class="container mx-auto max-w-[1500px] px-4 py-6">
  <div class="flex justify-between items-center mb-6">
    <div class="flex gap-4">
      <select class="select select-bordered" bind:value={selectedStrat}>
        <option value="">{m.all_strats()}</option>
        {#each strats as strat}
          <option value={strat}>{strat}</option>
        {/each}
      </select>

      <select class="select select-bordered" bind:value={selectedPeriod}>
        <option value="">{m.all_periods()}</option>
        {#each periods as period}
          <option value={period}>{period}</option>
        {/each}
      </select>

      <select class="select select-bordered" bind:value={selectedRange}>
        <option value={null}>{m.all_ranges()}</option>
        {#each ranges as range}
          <option value={range}>{range}</option>
        {/each}
      </select>

      <button class="btn" onclick={() => fetchTasks()}>{m.refresh()}</button>
    </div>

    <a class="btn btn-primary" href="/backtest/new">{m.run_backtest()}</a>
  </div>

  <!-- 结果列表 -->
  <div class="grid grid-cols-3 gap-6 mb-6">
    {#each tasks as task, tidx}
      <div class="card bg-base-100 shadow hover:shadow-lg transition-shadow duration-200 cursor-pointer relative" 
           onclick={() => clickTask(task.id)} onmouseenter={() => hoveredCard = tidx}
           onmouseleave={() => hoveredCard = null}>
        {#if hoveredCard === tidx}
          <div class="custom-tooltip {tidx >= 3 ? 'loc-top' : ''}">
            {task.status === 3 ? 
`ID: ${task.id}
Path: $/backtest/${task.path}
${m.max_open_orders()}: ${task.maxOpenOrders || '-'}
${m.bar_num()}: ${task.barNum || '-'}
${m.max_drawdown_val()}: ${task.maxDrawDownVal?.toFixed(2) || '-'}
${m.show_drawdown_val()}: ${task.showDrawDownVal?.toFixed(2) || '-'}
${m.show_drawdown_pct()}: ${task.showDrawDownPct?.toFixed(2) || '-'}%
${m.tot_fee()}: ${task.totFee?.toFixed(2) || '-'}` : 
`ID: ${task.id}
Path: $/backtest/${task.path}`}
          </div>
        {/if}
        <div class="card-body p-5">
          <!-- 第一行 -->
          <div class="flex justify-between items-center mb-3">
            <div class="flex items-center gap-3">
              <h2 class="text-lg font-bold" title={task.strats}>
                {#if task.strats.includes(',')}
                  {task.strats.split(',')[0] + '等' + task.strats.split(',').length + '个'}
                {:else}
                  {task.strats}
                {/if}
              </h2>
              <div class="text-sm opacity-60">{task.periods} · {showPairs(task.pairs)}</div>
            </div>
            {#if task.status === 3}
              <div class="text-2xl font-bold">{task.profitRate.toFixed(1)}%</div>
            {:else}
              <div class="badge badge-lg {task.status === 1 ? 'badge-neutral' : 'badge-primary'}">
                {task.status === 1 ? '等待开始' : '运行中'}
              </div>
            {/if}
          </div>

          <!-- 第二行 -->
          <div class="flex justify-between items-center mb-5">
            <div class="text-sm opacity-60">
              {new Date(task.startAt).toLocaleDateString()} - {new Date(task.stopAt).toLocaleDateString()}
            </div>
            {#if task.status === 3}
              <div class="text-sm opacity-80">{m.max_drawdown()}: {task.maxDrawdown.toFixed(1)}%</div>
            {/if}
          </div>
        
          <div class="grid grid-cols-4 gap-4 mb-4">
            <div>
              <div class="text-xs opacity-60 mb-1">{m.sharpe_ratio()}</div>
              <div class="text-base font-medium">{task.sharpe.toFixed(2)}</div>
            </div>
            <div>
              <div class="text-xs opacity-60 mb-1">{m.sortino_ratio()}</div>
              <div class="text-base font-medium">{task.sortinoRatio ? task.sortinoRatio.toFixed(2) : '-'}</div>
            </div>
            <div>
              <div class="text-xs opacity-60 mb-1">{m.order_num()}</div>
              <div class="text-base font-medium">{task.orderNum}</div>
            </div>
            <div>
              <div class="text-xs opacity-60 mb-1">{m.win_rate()}</div>
              <div class="text-base font-medium">{task.winRate.toFixed(1)}%</div>
            </div>
          </div>

          <div class="grid grid-cols-3 gap-2 text-xs opacity-60 pt-3 border-t border-base-200">
            <div>{m.leverage()}: {task.leverage}x</div>
            <div>{m.init_amount()}: {task.walletAmount}</div>
            <div>{m.stake_amount()}: {task.stakeAmount}</div>
          </div>
        </div>
      </div>
    {/each}
  </div>

  <!-- 分页器 -->
  {#if hasPrev || hasNext}
    <div class="flex justify-center">
      <div class="join">
        <button class="join-item btn" onclick={() => loadPage(false)} disabled={!hasPrev}>«</button>
        <button class="join-item btn" onclick={() => loadPage(true)} disabled={!hasNext}>»</button>
      </div>
    </div>
  {/if}
</div>