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
  import CodeMirror from './CodeMirror.svelte';
  import { oneDark } from '@codemirror/theme-one-dark';
  import type { Extension } from '@codemirror/state';
  import Icon from "$lib/Icon.svelte";

  let {
    show=$bindable(false),
  }: {
    show: boolean
  } = $props();
  
  let theme: Extension | null = $state(oneDark);
  let editor: CodeMirror | null = $state(null);
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
    endTime: '',
    concurrency: 4,
    importDir: '',
    yamlConfig: `klines:
  - exchange: 'binance'
    exg_real: ''
    market: 'spot'
    timeframes: ['1m', '5m', '15m', '1h', '1d']
    time_range: '20240101-20250101'
    symbols: []
#adj_factors:
#  - exchange: ''
#    exg_real: ''
#    market: ''
#    time_range: '20210101-20250101'
#    symbols: []
#calendars:
#  - exchange: ''
#    exg_real: ''
#    market: ''
#    time_range: '20210101-20250101'`
  });

  function setActiveTab(tab: string){
    activeTab = tab;
    if (tab === 'export'){
      setTimeout(() => {
        if(editor){
          editor.setValue('tmp.yml', form.yamlConfig);
        }
      }, 500)
    }
  }

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
          alerts.error(rsp.msg || 'get symbols failed');
          return;
        }else if(!rsp.data || rsp.data.length == 0){
          alerts.warning('no local symbols found');
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
    if ($site.heavyProgress > 0 && $site.heavyProgress < 1){
      alerts.error(m.heavy_task_busy());
      return;
    }
    // 构建请求参数
    let params: Record<string, any> = {
      action: activeTab,
      folder: activeTab === 'export' ? form.exportDir : form.importDir,
      exchange: form.exchange,
      exgReal: form.exgReal,
      market: form.market,
      pairs: form.symbols,
      periods: form.periods,
      startMs: form.startTime ? toUTCStamp(form.startTime) : 0,
      endMs: form.endTime ? toUTCStamp(form.endTime) : 0,
      concurrency: form.concurrency
    };

    if (activeTab === 'export') {
      params.config = form.yamlConfig;
    }

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
      alerts.error(rsp.msg || 'Operation failed');
      return;
    }
    alerts.success('Started successfully, please check the logs');
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

      {#if $site.heavyProgress > 0 && $site.heavyProgress < 1}
        <div class="flex items-center gap-2 mb-4 pr-4">
          <span>{$site.heavyName}</span>
          <progress class="flex-1 progress progress-info w-56" value={$site.heavyProgress} max=1></progress>
          <span>{($site.heavyProgress * 100).toFixed(1)}%</span>
        </div>
      {/if}

      <div role="tablist" class="tabs tabs-boxed mb-4">
        <a role="tab" 
           class="tab {activeTab === 'download' ? 'tab-active' : ''}"
           onclick={() => setActiveTab('download')}>{m.download()}</a>
        <a role="tab" 
           class="tab {activeTab === 'export' ? 'tab-active' : ''}"
           onclick={() => setActiveTab('export')}>{m.export_()}</a>
        <a role="tab"
           class="tab {activeTab === 'import' ? 'tab-active' : ''}"
           onclick={() => setActiveTab('import')}>{m.import_()}</a>
        <a role="tab" 
           class="tab {activeTab === 'purge' ? 'tab-active' : ''}"
           onclick={() => setActiveTab('purge')}>{m.purge()}</a>
        <a role="tab" 
           class="tab {activeTab === 'correct' ? 'tab-active' : ''}"
           onclick={() => setActiveTab('correct')}>{m.correct()}</a>
      </div>

      <div class="gap-4">
        {#if activeTab === 'export'}
          <fieldset class="fieldset">
            <label class="label" for="exportDir">{m.export_dir()}</label>
            <input id="exportDir" type="text" class="input w-full" placeholder={m.tip_export_data_dir()} bind:value={form.exportDir} />
          </fieldset>

          <fieldset class="fieldset">
            <label class="label" for="concurrency">{m.concurrency()}</label>
            <input id="concurrency" type="number" class="input" bind:value={form.concurrency} min="1" max="30"/>
          </fieldset>

          <fieldset class="fieldset">
            <div class="h-[400px]">
              <CodeMirror bind:this={editor} change={(v) => form.yamlConfig = v} {theme}/>
            </div>
          </fieldset>

          <div role="alert" class="alert">
            <Icon name="info"/>
            <span>{m.export_desc()}</span>
          </div>
        {/if}

        {#if activeTab === 'import'}
          <fieldset class="fieldset">
            <label class="label" for="importDir">{m.import_dir()}</label>
            <input id="importDir" type="text" class="input w-full" placeholder={m.tip_import_data_dir()} bind:value={form.importDir} />
          </fieldset>

          <fieldset class="fieldset">
            <label class="label" for="importConcurrency">{m.concurrency()}</label>
            <input id="importConcurrency" type="number" class="input" bind:value={form.concurrency} min="1" max="30"/>
          </fieldset>

          <div role="alert" class="alert">
            <Icon name="info"/>
            <span>{m.import_desc()}</span>
          </div>
        {/if}

        {#if ['download', 'purge', 'correct'].includes(activeTab)}
          <div class="flex gap-4">
            <fieldset class="fieldset flex-1">
              <label class="label" for="exchange">{m.exchange()}</label>
              <select id="exchange" class="select" required bind:value={form.exchange}>
                {#each exchanges as exchange}
                  <option value={exchange}>{exchange}</option>
                {/each}
              </select>
            </fieldset>

            {#if form.exchange == 'china'}
            <fieldset class="fieldset flex-1">
              <label class="label" for="exgReal">{m.exg_real()}</label>
              <input id="exgReal" type="text" class="input" bind:value={form.exgReal} />
            </fieldset>
            {/if}
          </div>

          <fieldset class="fieldset">
            <label class="label" for="market">{m.market()}</label>
            <select id="market" class="select" required bind:value={form.market}>
              <option value="">{m.any()}</option>
              {#each markets as market}
                <option value={market}>{market}</option>
              {/each}
            </select>
          </fieldset>

          <fieldset class="fieldset">
            <label class="label" for="symbols">{m.symbols_default_all()}</label>
            <SuggestTags bind:values={form.symbols} items={symbols} allowAny={true} />
          </fieldset>
        {/if}

        {#if ['download', 'purge'].includes(activeTab)}
          <fieldset class="fieldset">
            <legend>{m.periods()}</legend>
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
            <label class="label" for="symbols">{m.timeframe_save_desc()}</label>
          </fieldset>

          <fieldset class="fieldset">
            <legend>{m.time_range()}</legend>
            <div class="flex gap-2">
              <input 
                type="datetime-local" 
                class="input w-full"
                bind:value={form.startTime}
              />
              <input 
                type="datetime-local" 
                class="input w-full"
                bind:value={form.endTime}
              />
            </div>
          </fieldset>
        {/if}

        <button class="btn btn-primary mt-4" onclick={handleExecute}>
          {m.execute()}
        </button>
      </div>
    </div>
  </div>
</div>
