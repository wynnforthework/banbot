<script lang="ts">
  import * as m from '$lib/paraglide/messages.js'
  import CodeMirror from '$lib/dev/CodeMirror.svelte';
  import { oneDark } from '@codemirror/theme-one-dark';
  import type { Extension } from '@codemirror/state';
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import { getApi, postApi } from '@/lib/netio';
  import {alerts} from "$lib/stores/alerts"
  import AllConfig from '@/lib/dev/AllConfig.svelte';
  import {modals} from '$lib/stores/modals';
  import Modal from '@/lib/kline/Modal.svelte';
  import { i18n } from '$lib/i18n';

  let separateStrat = $state(false);
  let theme: Extension | null = $state(oneDark);
  let editor: CodeMirror | null = $state(null);
  let configDrawer = $state(false);
  let configText = $state('');
  let showDuplicate = $state(false);
  let dupMode = $state('');

  onMount(async () => {
    const rsp = await getApi('/dev/default_cfg');
    if(rsp.code != 200){
      alerts.addAlert("error", rsp.msg || 'get default config fail');
      console.error('get default config fail', rsp);
      return
    }
    if (editor) {
      configText = rsp.data || '';
      editor.setValue('config.yml', configText);
    }
  });

  async function onTextChange(value: string) {
    configText = value;
  }

  async function startBacktest() {
    if (!configText) {
      alerts.addAlert("error", "config is empty");
      return;
    }

    // 可以同时开始多个，后端会逐个启动执行
    const rsp = await postApi('/dev/run_backtest', {
      separate: separateStrat,
      config: configText,
      dupMode: dupMode
    });
    if (rsp.code === 400 && rsp.msg === "[-18] already_exist") {
      showDuplicate = true;
      return;
    }

    if (rsp.code === 200) {
      alerts.addAlert("success", m.add_bt_ok());
      goto(i18n.resolveRoute('/backtest'));
    } else {
      console.error('run backtest fail', rsp);
      alerts.addAlert("error", rsp.msg || "run backtest fail");
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
    const ok = await modals.confirm(m.confirm_backtest());
    if (!ok) return;
    startBacktest();
  }

</script>

<Modal title={m.duplicate_backtest()} buttons={['backup_start', 'overwrite_start', 'cancel']} show={showDuplicate} 
click={clickDuplicate} center={true} width={600}>
  {m.backtest_duplicate_info()}
</Modal>

<div class="drawer drawer-end">
  <input id="config-drawer" type="checkbox" class="drawer-toggle" bind:checked={configDrawer} />
  <div class="drawer-content">
    <div class="container mx-auto max-w-[1200px] px-4 py-6">
      <div class="flex justify-between items-center mb-6">
        <h2 class="text-2xl font-bold">{m.run_backtest()}</h2>
        <a class="btn btn-outline" href="/backtest">{m.back()}</a>
      </div>

      <!-- <div class="form-control mb-6">
        <div class="flex items-center gap-4">
          <span class="text-lg">{m.options()}: </span>
          <label class="label cursor-pointer">
            <span class="label-text mr-2">{m.separate_policy()}</span>
            <input type="checkbox" class="checkbox" bind:checked={separateStrat} />
          </label>
        </div>
      </div> -->

      <div class="mb-6">
        <div class="flex justify-between items-center mb-2">
          <h3 class="text-lg font-semibold">{m.config()}</h3>
          <label for="config-drawer" class="link link-primary cursor-pointer">{m.full_config()}</label>
        </div>
        <CodeMirror bind:this={editor} change={onTextChange} {theme} class="flex-1 h-full"/>
      </div>

      <div class="flex gap-4">
        <button class="btn btn-primary flex-1" onclick={clickBacktest}>{m.start_backtest()}</button>
        <a class="btn btn-outline flex-1" href="/backtest">{m.back()}</a>
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