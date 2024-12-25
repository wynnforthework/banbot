<script lang="ts">
  import { getApi, postApi } from '$lib/netio';
  import SuggestTags from '$lib/SuggestTags.svelte';
  import { markets, periods, exchanges } from '$lib/common';
  import {alerts} from '$lib/stores/alerts';
  import type { ExSymbol } from '$lib/dev/common';
  import {toUTCStamp} from '$lib/dateutil'
  import {modals} from '$lib/stores/modals';
  import * as m from '$lib/paraglide/messages.js';
  import {site} from "$lib/stores/site";

  let {
    show=$bindable(false),
  }: {
    show: boolean
  } = $props();
  
  let activeTab = $state('download');
  let symbols = $state<string[]>([]);

  // 工具表单数据
  let form = $state({
    exportDir: '',
    exchange: '',
    exgReal: '',
    market: '',
    symbols: [] as string[],
    periods: [] as string[],
    startTime: '',
    endTime: ''
  });

  $effect(() => {
    const exchange = form.exchange;
    const market = form.market;
    if(!exchange || !market) return;
    setTimeout(() => {
      symbols = [];
      getApi(`/dev/symbols`, {
        exchange: exchange,
        market: market,
        limit: 10000,
      }).then(rsp => {
        if(rsp.code != 200){
          console.error(rsp);
          alerts.addAlert('error', rsp.msg || 'get symbols failed');
          return;
        }else if(!rsp.data || rsp.data.length == 0){
          alerts.addAlert('warning', 'no local symbols found');
          return;
        }
        let res: string[] = [];
        rsp.data.forEach((item: ExSymbol) => {
          res.push(item.symbol);
        });
        symbols = res;
      });
    }, 0);
  });


  async function handleExecute() {
    // 构建请求参数
    let params: Record<string, any> = {
      action: activeTab,
      folder: form.exportDir,
      exchange: form.exchange,
      exgReal: form.exgReal,
      market: form.market,
      pairs: form.symbols,
      periods: form.periods,
      startMs: form.startTime ? toUTCStamp(form.startTime) : 0,
      endMs: form.endTime ? toUTCStamp(form.endTime) : 0
    };

    // 发送请求
    let rsp = await postApi('/dev/data_tools', params);
    if(rsp.code == 401) {
      const confirmMsg = m.confirm_run() + (rsp.msg || '');
      const confirmed = await modals.confirm(confirmMsg.replaceAll('\n', '<br>'));
      if(!confirmed) return;
      params.force = true;
      rsp = await postApi('/dev/data_tools', params);
    }
    if(rsp.code != 200) {
      console.error(rsp);
      alerts.addAlert('error', rsp.msg || 'Operation failed');
      return;
    }
    alerts.addAlert('success', 'Started successfully, please check the logs');
    show = false;
  }

</script>

<div class="drawer drawer-end">
  <input id="tool-drawer" type="checkbox" class="drawer-toggle" bind:checked={show} />
  <div class="drawer-side z-[100]">
    <label for="tool-drawer" aria-label="close sidebar" class="drawer-overlay"></label>
    <div class="bg-base-200 min-h-full w-[50%] p-4 pr-8">
      <!-- 工具内容 -->
      <div class="flex justify-between items-center mb-4">
        <h2 class="text-xl font-bold">{m.data_tools()}</h2>
        <button class="btn btn-ghost btn-sm" onclick={() => show = false}>✕</button>
      </div>

      {#if $site.heavyTotal > 0}
        <div class="flex items-center gap-2 mb-4">
          <span>{$site.heavyName}</span>
          <progress class="flex-1 progress progress-info w-56" value={$site.heavyDone} max={$site.heavyTotal}></progress>
          <span>{$site.heavyDone}/{$site.heavyTotal}</span>
        </div>
      {/if}

      <div role="tablist" class="tabs tabs-boxed mb-4">
        <a role="tab" 
           class="tab {activeTab === 'download' ? 'tab-active' : ''}"
           onclick={() => activeTab = 'download'}>{m.download()}</a>
        <a role="tab" 
           class="tab {activeTab === 'export' ? 'tab-active' : ''}"
           onclick={() => activeTab = 'export'}>{m.export_()}</a>
        <a role="tab" 
           class="tab {activeTab === 'purge' ? 'tab-active' : ''}"
           onclick={() => activeTab = 'purge'}>{m.purge()}</a>
        <a role="tab" 
           class="tab {activeTab === 'correct' ? 'tab-active' : ''}"
           onclick={() => activeTab = 'correct'}>{m.correct()}</a>
      </div>

      <div class="form-control gap-4">
        {#if activeTab === 'export'}
          <div class="form-control">
            <label class="label">
              <span class="label-text">{m.export_dir()}</span>
            </label>
            <input type="text" class="input input-bordered" bind:value={form.exportDir} />
          </div>
        {/if}

        {#if ['download', 'export', 'purge', 'correct'].includes(activeTab)}
          <div class="flex gap-4">
            <div class="form-control flex-1">
              <label class="label">
                <span class="label-text">{m.exchange()}</span>
              </label>
              <select class="select select-bordered" required bind:value={form.exchange}>
                {#each exchanges as exchange}
                  <option value={exchange}>{exchange}</option>
                {/each}
              </select>
            </div>

            {#if form.exchange == 'china'}
            <div class="form-control flex-1">
              <label class="label">
                <span class="label-text">{m.exg_real()}</span>
              </label>
              <input type="text" class="input input-bordered" bind:value={form.exgReal} />
            </div>
            {/if}
          </div>

          <div class="form-control">
            <label class="label">
              <span class="label-text">{m.market()}</span>
            </label>
            <select class="select select-bordered" required bind:value={form.market}>
              <option value="">{m.any()}</option>
              {#each markets as market}
                <option value={market}>{market}</option>
              {/each}
            </select>
          </div>

          <div class="form-control">
            <label class="label">
              <span class="label-text">{m.symbols_default_all()}</span>
            </label>
            <SuggestTags bind:values={form.symbols} items={symbols} allowAny={true} />
          </div>
        {/if}

        {#if ['download', 'export', 'purge'].includes(activeTab)}
          <div class="form-control">
            <label class="label">
              <span class="label-text">{m.periods()}</span>
            </label>
            <div class="flex flex-wrap gap-2">
              {#each periods as period}
                <label class="label cursor-pointer">
                  <input 
                    type="checkbox" 
                    class="checkbox checkbox-sm" 
                    bind:group={form.periods} 
                    value={period}
                  />
                  <span class="label-text ml-2">{period}</span>
                </label>
              {/each}
            </div>
          </div>
        {/if}

        {#if ['download', 'export', 'purge'].includes(activeTab)}
          <div class="form-control">
            <label class="label">
              <span class="label-text">{m.time_range()}</span>
            </label>
            <div class="flex gap-2">
              <input 
                type="datetime-local" 
                class="input input-bordered w-full" 
                bind:value={form.startTime}
              />
              <input 
                type="datetime-local" 
                class="input input-bordered w-full" 
                bind:value={form.endTime}
              />
            </div>
          </div>
        {/if}

        <button class="btn btn-primary mt-4" onclick={handleExecute}>
          {m.execute()}
        </button>
      </div>
    </div>
  </div>
</div>
