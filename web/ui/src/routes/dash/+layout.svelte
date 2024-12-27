<script lang="ts">
  import { page } from '$app/stores';
  import Icon from '$lib/Icon.svelte';
  import {ctx, save, acc, loadAccounts} from '$lib/dash/store';
  import type { BotAccount, BotTicket } from '$lib/dash/types';
  import * as m from '$lib/paraglide/messages.js';
	import { alerts } from '$lib/stores/alerts';
  import { loadBotAccounts } from '$lib/dash/store';
	import AddBot from '$lib/dash/AddBot.svelte';
  import Modal from '$lib/kline/Modal.svelte';
  import {site} from '@/lib/stores/site'
	
	let { children } = $props();
	let collapsed = $state(false);
  let showAddBot = $state(false);
  $site.apiReady = false;
  loadAccounts().then(() => {
    $site.apiHost = $acc.url;
    $site.apiReady = true;
    $site.apiReadyCbs.forEach(cb => cb());
    $site.apiReadyCbs = [];
  })
  
  const menuItems = [
    { path: '/dash/board', icon: 'home', text: m.home() },
    { path: '/dash/pair_job', icon: 'square-stack-3', text: m.pair_job() },
    { path: '/dash/kline', icon: 'chart-bar', text: m.kline() },
    { path: '/dash/perf', icon: 'calender', text: m.perf() },
    { path: '/dash/order', icon: 'number-list', text: m.order() },
    { path: '/dash/rebate', icon: 'dollar', text: m.rebate() },
    { path: '/dash/setting', icon: 'setting', text: m.config() },
    { path: '/dash/log', icon: 'document-text', text: m.logs() }
  ];

  function clickAccount(acc: BotAccount) {
    $save.current = `${acc.url}_${acc.account}`;
    location.reload();
  }
  
  function loginOk(info: BotTicket) {
    const num = Object.keys(info.accounts!).length
    alerts.addAlert('success', `${m.add_bot_ok()}: ${info.name} (${num} accounts)`);
    showAddBot = false;
    loadBotAccounts(info);
  }
</script>

<Modal title={m.add_bot()} bind:show={showAddBot} buttons={[]}>
  <AddBot newBot={loginOk}/>
</Modal>

<div class="flex h-screen" onclick={() => $ctx.clickPage += 1}>
  <aside class="bg-neutral text-neutral-content shadow-xl {collapsed ? 'w-16' : 'w-64'} transition-all duration-300">
    <div class="p-4 flex justify-center items-center">
      <a href="/dash" class="text-xl font-bold">{collapsed ? 'Ban' : 'Banbot'}</a>
    </div>
    
    <ul class="menu menu-vertical">
      {#each menuItems as {path, icon, text}}
        <li>
          <a 
            href={path} 
            class:bg-primary={$page.url.pathname === path}
            class:text-primary-content={$page.url.pathname === path}
            class:font-bold={$page.url.pathname === path}
          >
            <Icon name={icon} />
            {#if !collapsed}
              <span>{text}</span>
            {/if}
          </a>
        </li>
      {/each}
    </ul>
  </aside> 
	
	<div class="flex-1 flex flex-col bg-base-200">
    <header class="navbar bg-base-100 shadow-md h-12">
      <div class="navbar-start">
        <button class="btn btn-ghost btn-circle" onclick={() => collapsed = !collapsed}>
          <Icon name={collapsed ? 'double-right' : 'double-left'} />
        </button>
      </div>
      
      <div class="navbar-center">
        <ul class="menu menu-horizontal px-1">
          <li>
            <details>
              <summary>{$acc.name+'/'+$acc.account}</summary>
              <ul class="p-2 bg-base-100 rounded-box shadow-xl z-10">
                {#each $ctx.accounts as acc}
                  <li>
                    <a 
                      class={`${acc.url}_${acc.account}` === $save.current ? 'active' : ''} 
                      onclick={() => clickAccount(acc)}
                    >
                      {acc.name}/{acc.account}
                    </a>
                  </li>
                {/each}
              </ul>
            </details>
          </li>
        </ul>
      </div>
      
      <div class="navbar-end">
        <ul class="menu menu-horizontal px-1">
          <li><a href="/dash">{m.bot_list()}</a></li>
          <li><a onclick={() => showAddBot = true}>{m.add_bot()}</a></li>
        </ul>
      </div>
    </header>
		
		<main class="flex-1 p-4 flex flex-col">
			<!-- <div class="bg-base-100 rounded-lg shadow-lg p-6 min-h-[600px]"> -->
				{@render children()}
			<!-- </div> -->
		</main>
	</div>
</div>
