<script lang="ts">
  import { onMount } from 'svelte';
  import * as m from '$lib/paraglide/messages.js'
  import type { BotTicket, BotAccount } from '$lib/dash/types';
  import AddBot from '$lib/dash/AddBot.svelte';
  import { alerts } from '$lib/stores/alerts';
  import { modals } from '$lib/stores/modals';
  import { ctx, save, loadBotAccounts, loadAccounts } from '$lib/dash/store';

  let showAdd = false;
  let activeTab = 'accounts';

  async function loginOk(info: BotTicket) {
    const num = Object.keys(info.accounts!).length
    alerts.addAlert('success', `${m.add_bot_ok()}: ${info.name} (${num} accounts)`);
    showAdd = false;
    await loadBotAccounts(info);
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
  }

  onMount(async () => {
    await loadAccounts(true);
  });
</script>

<div class="min-h-screen bg-base-100 flex items-center justify-center py-12">
  <div class="container max-w-[1100px] mx-auto px-3">
    <div class="text-center space-y-4 mb-10">
      <h1 class="text-4xl font-bold text-primary/90">Banbot</h1>
      <div class="space-y-3">
        <p class="text-lg text-base-content/80 my-6">{m.dash_desc()}</p>
        <div class="flex justify-center items-center gap-2 text-sm text-base-content/70">
          <span>{m.dash_help()}</span>
          <a class="link link-primary hover:link-accent transition-colors duration-200" 
             href="https://docs.banbot.site/">{m.our_doc()}</a>
        </div>
      </div>
    </div>

    {#if $save.tickets.length > 0}
    <div class="card bg-base-100 shadow-md border border-base-200 rounded-lg">
      <div class="card-body p-4">
        <div role="tablist" class="tabs tabs-boxed bg-base-200/50 p-1 mb-4 rounded-md">
          <a role="tab" 
             class="tab tab-sm {activeTab === 'accounts' ? 'tab-active bg-primary/90 text-primary-content' : ''}"
             onclick={() => activeTab = 'accounts'}>
            {m.trading_accounts()}
          </a>
          <a role="tab" 
             class="tab tab-sm {activeTab === 'tickets' ? 'tab-active bg-primary/90 text-primary-content' : ''}"
             onclick={() => activeTab = 'tickets'}>
            {m.login_credentials()}
          </a>
        </div>

        {#if activeTab === 'accounts'}
          <div class="overflow-x-auto">
            <table class="table table-sm">
              <thead>
                <tr class="bg-base-200/30 text-sm">
                  <th class="font-medium">{m.account()}</th>
                  <th class="font-medium">{m.role()}</th>
                  <th class="font-medium">{m.status()}</th>
                  <th class="font-medium text-right">{m.today_done()}</th>
                  <th class="font-medium text-right">{m.hold_pos()}</th>
                  <th class="font-medium">{m.actions()}</th>
                </tr>
              </thead>
              <tbody>
                {#each $ctx.accounts as acc}
                  <tr class="hover:bg-base-200/50">
                    <td>
                      <div class="tooltip tooltip-right" data-tip={acc.url}>
                        <span class="font-mono text-sm">{acc.name}/{acc.account}</span>
                      </div>
                    </td>
                    <td class="text-sm">{acc.role}</td>
                    <td>
                      <span class="badge badge-sm {acc.running ? 'badge-success' : 'badge-warning'} font-normal">
                        {acc.running ? m.running() : m.stopped()}
                      </span>
                    </td>
                    <td class="text-right font-mono text-sm">{acc.dayDonePft?.toFixed(2)}[{acc.dayDoneNum}]</td>
                    <td class="text-right font-mono text-sm">{acc.dayOpenPft?.toFixed(2)}[{acc.dayOpenNum}]</td>
                    <td>
                      <a class="btn btn-xs btn-primary" href={localizeHref("/dash/board")} onclick={() => viewAccount(acc)}>
                        {m.view()}
                      </a>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {:else}
          <div class="overflow-x-auto">
            <table class="table table-sm">
              <thead>
                <tr class="bg-base-200/30 text-sm">
                  <th class="font-medium">{m.username()}</th>
                  <th class="font-medium">{m.bot_name()}</th>
                  <th class="font-medium">{m.account()}</th>
                  <th class="font-medium">{m.actions()}</th>
                </tr>
              </thead>
              <tbody>
                {#each $save.tickets as ticket}
                  <tr class="hover:bg-base-200/50">
                    <td>
                      <div class="tooltip tooltip-right" data-tip={ticket.url}>
                        <span class="font-mono text-sm">{ticket.user_name}</span>
                      </div>
                    </td>
                    <td class="text-sm">{ticket.name}</td>
                    <td>
                      {#if ticket.accounts}
                        <div class="whitespace-pre-wrap font-mono text-xs opacity-80">
                          {Object.entries(ticket.accounts)
                            .map(([acc, role]) => `${acc}: ${role}`)
                            .join('\n')}
                        </div>
                      {/if}
                    </td>
                    <td class="flex gap-1.5">
                      <button class="btn btn-xs btn-error" onclick={() => delTicket(ticket)}>
                        {m.remove()}
                      </button>
                      <button class="btn btn-xs btn-primary" onclick={() => showAdd = true}>
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

    <div class="text-center mt-6">
      {#if !showAdd}
        <button class="btn btn-primary btn-sm gap-1.5 px-6" onclick={() => showAdd = true}>
          <i class="fas fa-plus text-xs"></i>
          {m.login_bot()}
        </button>
      {:else}
        <AddBot newBot={loginOk} />
      {/if}
    </div>
  </div>
</div>

