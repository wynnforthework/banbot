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

  let separateStrat = $state(false);
  let theme: Extension | null = $state(oneDark);
  let editor: CodeMirror | null = $state(null);
  let configDrawer = $state(false);
  let configText = $state('');

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

    const ok = await modals.confirm(m.confirm_backtest());
    if (!ok) return;

    const rsp = await postApi('/dev/run_backtest', {
      separate: separateStrat,
      config: configText
    });

    if (rsp.code === 200) {
      alerts.addAlert("success", m.add_bt_ok());
      goto('/backtest');
    } else {
      console.error('run backtest fail', rsp);
      alerts.addAlert("error", rsp.msg || "run backtest fail");
    }
  }

  function goBack() {
    goto('/backtest');
  }
</script>

<div class="drawer drawer-end">
  <input id="config-drawer" type="checkbox" class="drawer-toggle" bind:checked={configDrawer} />
  <div class="drawer-content">
    <div class="container mx-auto max-w-[1200px] px-4 py-6">
      <div class="flex justify-between items-center mb-6">
        <h2 class="text-2xl font-bold">{m.run_backtest()}</h2>
        <button class="btn btn-outline" onclick={goBack}>{m.back()}</button>
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
        <button class="btn btn-primary flex-1" onclick={startBacktest}>{m.start_backtest()}</button>
        <button class="btn btn-outline flex-1" onclick={goBack}>{m.back()}</button>
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