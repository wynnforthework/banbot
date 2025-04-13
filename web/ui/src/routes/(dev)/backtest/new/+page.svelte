<script lang="ts">
  import * as m from '$lib/paraglide/messages.js'
  import CodeMirror from '$lib/dev/CodeMirror.svelte';
  import { oneDark } from '@codemirror/theme-one-dark';
  import type { Extension } from '@codemirror/state';
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { getApi, postApi } from '$lib/netio';
  import {alerts} from "$lib/stores/alerts"
  import AllConfig from '$lib/dev/AllConfig.svelte';
  import Modal from '$lib/kline/Modal.svelte';
  import {localizeHref} from "$lib/paraglide/runtime";
  import { site } from '$lib/stores/site';
  import {clickCompile} from "$lib/dev/common";

  let separateStrat = $state(false);
  let theme: Extension | null = $state(oneDark);
  let editor: CodeMirror | null = $state(null);
  let configDrawer = $state(false);
  let configText = $state('');
  let showDuplicate = $state(false);
  let showCompile = $state(false);
  let disableMainBtn = $state(false);
  let dupMode = $state('');
  let activeTab = $state('');
  let tabs: Record<string, string> = $state({});
  let strats: string[] = $state([]);
  let searchQuery = $state('');

  let filteredStrats: string[] = $derived.by(() => {
    if (!searchQuery) return strats;
    return strats.filter(strat => 
      strat.toLowerCase().includes(searchQuery.toLowerCase())
    );
  });

  onMount(async () => {
    let rsp = await getApi('/dev/available_strats');
    if(rsp.code != 200) {
      alerts.error(rsp.msg || 'load strats failed');
      return;
    }
    strats = rsp.data;
    let arr = ['config.yml', 'config.local.yml'];
    let paths = arr.map(v => "@" + v);
    rsp = await getApi('/dev/texts', { paths });
    if(rsp.code != 200) {
      alerts.error(rsp.msg || 'load config failed');
      return;
    }
    paths.forEach(p => {
      if(rsp[p]){
        activeTab = p.substring(1);
        tabs[activeTab] = rsp[p];
        configText = rsp[p];
      }
    })
    if (editor) {
      editor.setValue(activeTab, configText);
    }
  });

  $effect(() => {
    if(activeTab){
      setTimeout(function () {
        configText = tabs[activeTab];
        editor?.setValue(activeTab, configText);
      }, 100)
    }
  });

  async function onTextChange(value: string) {
    configText = value;
    tabs[activeTab] = value;
  }

  async function startBacktest() {
    if (!configText) {
      alerts.error("config is empty");
      return;
    }

    // 可以同时开始多个，后端会逐个启动执行
    const configs: Record<string, string> = {};
    for (const key in tabs) {
      if (tabs.hasOwnProperty(key)) {
        configs[`@${key}`] = tabs[key];
      }
    }
    const rsp = await postApi('/dev/run_backtest', {
      separate: separateStrat,
      configs: configs,
      dupMode: dupMode
    });
    if (rsp.code === 400 && rsp.msg === "[-18] already_exist") {
      showDuplicate = true;
      return;
    }

    if (rsp.code === 200) {
      alerts.success(m.add_bt_ok());
      goto(localizeHref('/backtest'));
    } else {
      console.error('run backtest fail', rsp);
      alerts.error(rsp.msg || "run backtest fail");
    }
  }

  async function clickDuplicate(type: string) {
    showDuplicate = false;
    if (type === 'backup_start') {
      dupMode = 'backup';
    }else if (type === 'overwrite_start') {
      dupMode = 'overwrite';
    }else{
      return;
    }
    startBacktest();
  }

  async function clickBacktest(){
    if($site.dirtyBin){
      showCompile = true
    }else{
      await startBacktest();
    }
  }

  async function clickCompileOrNot(choose: string){
    showCompile = false;
    if(choose == 'compile'){
      disableMainBtn = true;
      await clickCompile()
      disableMainBtn = false;
    }else if(choose == 'just_backtest'){
      $site.dirtyBin = false;
      await startBacktest();
    }
  }

  async function copyToClipboard(text: string) {
    await navigator.clipboard.writeText(text);
    alerts.success(m.copied());
  }

</script>

<Modal title={m.duplicate_backtest()} buttons={['backup_start', 'overwrite_start', 'cancel']} show={showDuplicate} 
click={clickDuplicate} center={true} width={600}>
  {m.backtest_duplicate_info()}
</Modal>

<Modal title={m.confirm()} buttons={['compile', 'just_backtest', 'cancel']} show={showCompile}
       click={clickCompileOrNot} center={true} width={400}>
  {m.build_for_backtest()}
</Modal>

<div class="drawer drawer-end">
  <input id="config-drawer" type="checkbox" class="drawer-toggle" bind:checked={configDrawer} />
  <div class="drawer-content">
    <div class="container mx-auto px-4 py-6">
      <div class="flex gap-6">
        <!-- 左侧策略列表 -->
        <div class="w-[15%] bg-base-200 rounded-lg p-3">
          <h2 class="text-lg font-bold mb-2 text-primary">{m.registered_strats()}</h2>
          <div class="mb-3">
            <input type="text" placeholder="Search ..." class="input input-sm" bind:value={searchQuery}/>
          </div>
          <div class="space-y-0.5">
            {#each filteredStrats as strat}
              <div 
                class="px-2 py-1 rounded cursor-pointer hover:bg-primary hover:bg-opacity-10 transition-all duration-200 text-sm"
                onclick={() => copyToClipboard(strat)}
              >
                {strat}
              </div>
            {/each}
          </div>
        </div>

        <!-- 右侧主内容区 -->
        <div class="flex-1">
          <div class="flex justify-between items-center mb-6">
            <h2 class="text-2xl font-bold">{m.run_backtest()}</h2>
            <a class="btn btn-outline" href={localizeHref("/backtest")}>{m.backtest_history()}</a>
          </div>

          <div class="mb-12">
            <div class="flex justify-between items-center mb-2">
              <div>
                <div class="tabs tabs-box tabs-sm">
                  {#each Object.keys(tabs) as tab}
                    <input type="radio" class="tab" aria-label={tab} checked={activeTab === tab}
                           onclick={() => activeTab = tab}/>
                  {/each}
                </div>
                <p class="mt-2 text-sm opacity-70">
                  {activeTab === 'config.local.yml'
                          ? m.local_config_desc()
                          : m.global_config_desc()}
                </p>
              </div>
              <label for="config-drawer" class="link link-primary cursor-pointer">{m.full_config()}</label>
            </div>
            <CodeMirror bind:this={editor} change={onTextChange} {theme} class="flex-1 h-full"/>
          </div>

          <div class="flex gap-4 fixed bottom-0 left-0 right-0 p-2 w-[100%] bg-white flex justify-center">
            <button class="btn btn-primary w-[50%]" disabled={disableMainBtn} onclick={clickBacktest}>{m.start_backtest()}</button>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div class="drawer-side">
    <label for="config-drawer" aria-label="close sidebar" class="drawer-overlay"></label>
    <div class="bg-base-200 min-h-full w-2/3 p-4">
      <AllConfig />
    </div>
  </div>
</div>