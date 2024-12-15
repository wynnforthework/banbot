<script lang="ts">
  import { onMount } from 'svelte';
  import { goto } from '$app/navigation';
  import * as m from '$lib/paraglide/messages.js'
  import type { BotTicket, BotAccount } from '$lib/dash/types';
  import AddBot from '$lib/dash/addBot.svelte';
  import { alerts } from '$lib/stores/alerts';
  import { modals } from '$lib/stores/modals';
  import { ctx, save, loadBotAccounts, loadAccounts } from '$lib/dash/store';

  let showAdd = false;
  let activeTab = 'accounts';

  function loginOk(info: BotTicket) {
    const num = Object.keys(info.accounts!).length
    alerts.addAlert('success', `${m.add_bot_ok()}: ${info.name} (${num} accounts)`);
    showAdd = false;
    loadBotAccounts(info);
  }

  async function delTicket(ticket: BotTicket) {
    if (!await modals.confirm(m.confirm_logout_bot())) return;
    
    save.update(s => {
      s.tickets = s.tickets.filter(t => t.url !== ticket.url);
      return s;
    });
    
    ctx.update(c => {
      c.accounts = [];
      return c;
    });
    await loadAccounts(true);
  }

  function viewAccount(account: BotAccount) {
    $save.current = `${account.url}_${account.account}`;
    goto('/dash/board');
  }

  onMount(async () => {
    await loadAccounts(true);
  });
</script>

<div class="min-h-screen bg-base-200 flex items-center justify-center py-16">
  <div class="container max-w-[1200px] mx-auto px-4">
    <div class="text-center space-y-6 mb-12">
      <h1 class="text-5xl font-bold text-primary">Banbot</h1>
      <div class="space-y-4">
        <p class="text-xl text-base-content/80 my-8">{m.dash_desc()}</p>
        <div class="flex justify-center items-center gap-3 text-md text-base-content/70">
          <span>{m.dash_help()}</span>
          <a class="link link-primary hover:link-accent transition-colors duration-200" 
             href="https://www.banbox.top/">{m.document()}</a>
        </div>
      </div>
    </div>

    {#if $save.tickets.length > 0}
    <div class="card bg-base-100 shadow-xl">
      <div class="card-body">
        <div role="tablist" class="tabs tabs-boxed mb-4">
          <a role="tab" 
             class="tab {activeTab === 'accounts' ? 'tab-active' : ''}"
             on:click={() => activeTab = 'accounts'}>
            {m.trading_accounts()}
          </a>
          <a role="tab" 
             class="tab {activeTab === 'tickets' ? 'tab-active' : ''}"
             on:click={() => activeTab = 'tickets'}>
            {m.login_credentials()}
          </a>
        </div>

        {#if activeTab === 'accounts'}
          <div class="overflow-x-auto">
            <table class="table">
              <thead>
                <tr>
                  <th>{m.account()}</th>
                  <th>{m.role()}</th>
                  <th>{m.status()}</th>
                  <th>{m.today_done()}</th>
                  <th>{m.today_done_profit()}</th>
                  <th>{m.open_num()}</th>
                  <th>{m.upol_profit()}</th>
                  <th>{m.actions()}</th>
                </tr>
              </thead>
              <tbody>
                {#each $ctx.accounts as acc}
                  <tr class="hover">
                    <td>
                      <div class="tooltip" data-tip={acc.url}>
                        {acc.account}
                      </div>
                    </td>
                    <td>{acc.role}</td>
                    <td>{acc.running ? m.running() : m.stopped()}</td>
                    <td>{acc.dayDoneNum}</td>
                    <td>{acc.dayDonePft?.toFixed(2)}</td>
                    <td>{acc.dayOpenNum}</td>
                    <td>{acc.dayOpenPft?.toFixed(2)}</td>
                    <td>
                      <button class="btn btn-sm btn-primary"
                              on:click={() => viewAccount(acc)}>
                        {m.view()}
                      </button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {:else}
          <div class="overflow-x-auto">
            <table class="table">
              <thead>
                <tr>
                  <th>{m.username()}</th>
                  <th>{m.bot_name()}</th>
                  <th>{m.account()}</th>
                  <th>{m.actions()}</th>
                </tr>
              </thead>
              <tbody>
                {#each $save.tickets as ticket}
                  <tr class="hover">
                    <td>
                      <div class="tooltip" data-tip={ticket.url}>
                        {ticket.user_name}
                      </div>
                    </td>
                    <td>{ticket.name}</td>
                    <td>
                      {#if ticket.accounts}
                        <div class="whitespace-pre-wrap font-mono text-sm">
                          {Object.entries(ticket.accounts)
                            .map(([acc, role]) => `${acc}: ${role}`)
                            .join('\n')}
                        </div>
                      {/if}
                    </td>
                    <td class="flex gap-2">
                      <button class="btn btn-sm btn-error" on:click={() => delTicket(ticket)}>
                        {m.remove()}
                      </button>
                      <button class="btn btn-sm btn-primary" on:click={() => showAdd = true}>
                        {m.edit()}
                      </button>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    </div>
    {/if}

    <div class="text-center mt-8">
      {#if !showAdd}
        <button class="btn btn-primary btn-wide gap-2" 
                on:click={() => showAdd = true}>
          <i class="fas fa-plus"></i>
          {m.login_bot()}
        </button>
      {:else}
        <AddBot newBot={loginOk} />
      {/if}
    </div>
  </div>
</div>

