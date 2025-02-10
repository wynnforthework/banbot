<script lang="ts">
  import { onMount } from 'svelte';
  import { getApi } from '$lib/netio';
  import {alerts} from '$lib/stores/alerts';
  import type {BtTask} from "$lib/dev/types"
  import * as m from '$lib/paraglide/messages.js'
  import { goto } from '$app/navigation';
  import { showPairs } from '$lib/dev/common';
  import { fmtDateStr } from '$lib/dateutil';
  import {addListener} from '$lib/dev/websocket';
  import { i18n } from '$lib/i18n';
  import {site} from "$lib/stores/site";

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
  let loading = $state(false);

  let isMultiSelect = $state(false);
  let selectedTasks = $state<BtTask[]>([]);
  let compareUrl = $state('');

  let pageSize = 12;

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
    if(loading) return;
    await doFetchTasks(maxId, true);
  }

  async function doFetchTasks(maxId?: number, show: boolean = true) {
    if(show) loading = true;
    const rsp = await getApi('/dev/bt_tasks', {
      mode: 'backtest',
      strat: selectedStrat,
      period: selectedPeriod,
      range: selectedRange,
      limit: pageSize,
      maxId: maxId || 0
    });
    if(show) loading = false;
    if(rsp.code != 200) {
      console.error('load backtest tasks failed', rsp);
      alerts.addAlert('error', rsp.msg || 'load backtest tasks failed');
      return;
    }
    tasks = rsp.data;
    hasPrev = !!(maxId && maxId > 0);
    hasNext = tasks.length >= pageSize;
    let nearDone = false;
    tasks.forEach(task => {
      if(task.status >= 3)return;
      if(task.progress >= 0.99999){
        nearDone = true;
      }
      if(show){
        addListener(`btPrg_${task.id}`, (res) => {
          task.progress = res.progress;
          if(task.status == 1){
            task.status = 2
          }
          if(task.progress >= 0.99999){
            setTimeout(() => {
              doFetchTasks(undefined, false);
            }, 300);
          }
        });
      }
    });
    if(nearDone){
      setTimeout(() => {
        doFetchTasks(undefined, false);
      }, 300);
    }
  }

  function showStrats(strats: string) {
    if(strats.includes(',')) {
      const parts = strats.split(',');
      return parts[0] + ' & ' + (parts.length - 1) + ' More';
    }
    return strats;
  }

  function taskTooltip(task: BtTask){
    var base = `ID: ${task.id}
Path: $/backtest/${task.path}`
    if(task.status === 3){
      return base + `
${m.max_open_orders()}: ${task.maxOpenOrders || '-'}
${m.bar_num()}: ${task.barNum || '-'}
${m.max_drawdown_val()}: ${task.maxDrawDownVal?.toFixed(2) || '-'}
${m.show_drawdown_val()}: ${task.showDrawDownVal?.toFixed(2) || '-'}
${m.show_drawdown()}: ${task.showDrawDownPct?.toFixed(2) || '-'}%
${m.tot_fee()}: ${task.totFee?.toFixed(2) || '-'}`
    }else if(task.status === 4){
      return base + `
Error: ${task.info}`
    }
    return base
  }

  $effect(() => {
    // 这行不能删除，用于监控变化
    console.log('filter changed', selectedStrat, selectedPeriod, selectedRange);
    setTimeout(() => {
      fetchTasks();
    }, 0)
  });

  function clickTask(task: BtTask) {
    if(!task.path) {
      alerts.addAlert('warning', m.bt_result_not_exist());
    }else{
      goto(i18n.resolveRoute(`/backtest/item?id=${task.id}`))
    }
  }

  function toggleMultiSelect() {
    isMultiSelect = !isMultiSelect;
    selectedTasks = [];
    compareUrl = '';
  }

  function toggleTaskSelection(task: BtTask, event: MouseEvent) {
    if (!isMultiSelect) return;
    event.preventDefault();
    event.stopPropagation();
    
    const idx = selectedTasks.findIndex(t => t.id === task.id);
    if (idx >= 0) {
      selectedTasks = selectedTasks.filter(t => t.id !== task.id);
    } else {
      selectedTasks = [...selectedTasks, task];
    }
    
    // 更新对比URL
    if (selectedTasks.length >= 2) {
      const ids = selectedTasks.map(t => t.id).join(',');
      compareUrl = `${$site.apiHost}/api/dev/compare_assets?ids=${ids}`;
    } else {
      compareUrl = '';
    }
  }

  function isTaskSelected(task: BtTask) {
    return selectedTasks.some(t => t.id === task.id);
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

      <button class="btn" disabled={loading} onclick={() => fetchTasks()}>
        {#if loading}
        <span class="loading loading-spinner"></span>
        {/if}
        {m.refresh()}
      </button>
    </div>

    <div class="flex gap-4">
      <button class="btn" onclick={toggleMultiSelect}>
        {isMultiSelect ? m.exit_select() : m.multi_select()}
      </button>
      {#if !isMultiSelect}
        <a class="btn btn-primary" href="/backtest/new">{m.run_backtest()}</a>
      {:else if compareUrl}
        <a class="btn btn-primary" href={compareUrl} target="_blank">{m.compare_assets()}</a>
      {/if}
    </div>
  </div>

  <!-- 结果列表 -->
  <div class="grid grid-cols-3 gap-6 mb-6">
    {#each tasks as task, tidx}
      <div class="card bg-base-100 shadow hover:shadow-lg transition-shadow duration-200 cursor-pointer relative {isTaskSelected(task) ? 'ring-2 ring-primary' : ''}"
           onclick={(e) => isMultiSelect ? toggleTaskSelection(task, e) : clickTask(task)} 
           onmouseenter={() => hoveredCard = tidx}
           onmouseleave={() => hoveredCard = null}>
        {#if hoveredCard === tidx}
          <div class="custom-tooltip {tidx >= 3 ? 'loc-top' : ''}">{taskTooltip(task)}</div>
        {/if}
        <div class="card-body p-5">
          <!-- 第一行 -->
          <div class="flex justify-between items-center mb-3">
            <div class="flex items-center gap-3">
              <h2 class="text-lg font-bold" title={task.strats}>
                {showStrats(task.strats)}
              </h2>
              <div class="text-sm opacity-60">{task.periods}</div>
            </div>
            {#if task.status === 3}
              <div class="text-2xl font-bold">{task.profitRate.toFixed(1)}%</div>
            {:else if task.status === 4}
              <div class="badge badge-lg badge-error gap-2">
                <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" class="inline-block w-4 h-4 stroke-current"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path></svg>
              </div>
            {:else}
              {#if task.status === 1}
                <div class="badge badge-lg badge-neutral">{m.pending()}</div>
              {:else}
                <div class="radial-progress text-primary" style="--value:{(task.progress || 0) * 100}; --size:2.5rem; --thickness: 2px;" role="progressbar">
                  {((task.progress || 0) * 100).toFixed(0)}%
                </div>
              {/if}
            {/if}
          </div>

          <!-- 第二行 -->
          <div class="flex justify-between items-center mb-5">
            <div class="text-sm opacity-60">
              {fmtDateStr(task.startAt, 'YYYYMMDD')} - {fmtDateStr(task.stopAt, 'YYYYMMDD')}
            </div>
            <div class="text-sm opacity-60">{showPairs(task.pairs)}</div>
            {#if task.status === 3}
              <div class="text-sm opacity-80">{m.max_drawdown_short()}: {task.maxDrawdown.toFixed(1)}%</div>
            {:else if task.status === 4}
              <div class="text-sm text-error">{task.info}</div>
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