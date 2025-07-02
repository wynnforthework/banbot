<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import * as m from '$lib/paraglide/messages.js'
  import type { BotTicket } from '$lib/dash/types';
  import AddBot from '$lib/dash/AddBot.svelte';
  import { alerts } from '$lib/stores/alerts';
  import { modals } from '$lib/stores/modals';
  import {ctx, save, loadAccounts, activeAcc} from '$lib/dash/store';
  import {localizeHref, getLocale} from "$lib/paraglide/runtime";
  import { goto } from '$app/navigation';

  let showAdd = $state(false);
  let activeTab = $state('accounts');
  let pendingAuthData: any = null;

  // iframe 通信状态管理
  let iframeCommStatus = $state({
    isInIframe: false,
    requestSent: false,
    authReceived: false,
    authProcessed: false,
    error: null as string | null,
    lastMessageTime: null as number | null,
    messageHistory: [] as Array<{type: string, data: any, timestamp: number}>
  });

  function loginOk(info: BotTicket) {
    const num = Object.keys(info.accounts!).length
    alerts.success(`${m.add_bot_ok()}: ${info.name} (${num} accounts)`);
    showAdd = false;
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

  function accStateText(status: string | undefined): string{
    switch(status){
      case 'running': return m.running();
      case 'stopped': return m.stopped();
      case 'disconnected': return m.disconnected();
      default: return "unknown";
    }
  }

  function accStateClass(status: string | undefined): string{
    switch(status){
      case 'running': return 'badge-success';
      case 'stopped': return 'badge-warning';
      case 'disconnected': return 'badge-neutral';
      default: return "";
    }
  }

  // 添加debug日志函数
  function logDebug(message: string, data?: any) {
    const timestamp = new Date().toISOString();
    console.debug(`[BOT-DASH] ${timestamp}: ${message}`, data || '');

    // 记录到消息历史
    iframeCommStatus.messageHistory.push({
      type: 'debug',
      data: { message, data },
      timestamp: Date.now()
    });

    // 保持历史记录在合理范围内
    if (iframeCommStatus.messageHistory.length > 20) {
      iframeCommStatus.messageHistory = iframeCommStatus.messageHistory.slice(-20);
    }
  }

  // 处理来自父窗口的消息
  function handleParentMessage(event: MessageEvent) {
    const timestamp = Date.now();
    iframeCommStatus.lastMessageTime = timestamp;

    logDebug('Received message from parent', {
      type: event.data.type,
      origin: event.origin,
      data: event.data
    });

    // 记录消息到历史
    iframeCommStatus.messageHistory.push({
      type: 'received',
      data: event.data,
      timestamp
    });

    if (event.data.type === 'AUTH_DATA') {
      // 接收到鉴权数据
      pendingAuthData = event.data.data;
      iframeCommStatus.authReceived = true;
      logDebug('Auth data received successfully', pendingAuthData);

      // 处理自动登录
      handleAutoLogin();

      // 通知父窗口鉴权成功
      if (window.parent) {
        const successMessage = {
          type: 'AUTH_SUCCESS'
        };
        window.parent.postMessage(successMessage, '*');
        logDebug('Sent AUTH_SUCCESS to parent', successMessage);
      }
    }
  }

  // 处理自动登录
  async function handleAutoLogin() {
    if (!pendingAuthData || !pendingAuthData.dashKey) {
      iframeCommStatus.error = 'No auth data or dashKey missing';
      logDebug('Auto login failed: missing auth data or dashKey', pendingAuthData);
      return;
    }

    try {
      logDebug('Starting auto login process', pendingAuthData);

      // 根据接收到的鉴权数据构造 BotTicket
      const ticket: BotTicket = {
        url: location.origin,
        user_name: pendingAuthData.username,
        password: '', // 不需要密码
        env: pendingAuthData.env,
        market: pendingAuthData.market,
        name: pendingAuthData.name,
        token: pendingAuthData.token,
        accounts: pendingAuthData.accounts
      };

      logDebug('Constructed ticket', ticket);

      // 添加或更新票据到本地存储
      save.update(s => {
        const existingIndex = s.tickets.findIndex(t => t.url === ticket.url);
        if (existingIndex >= 0) {
          s.tickets[existingIndex] = ticket;
          logDebug('Updated existing ticket');
        } else {
          s.tickets.push(ticket);
          logDebug('Added new ticket');
        }
        return s;
      });

      // 创建账户信息并更新上下文
      const accounts: any[] = [];
      if (ticket.accounts) {
        for (const [account, role] of Object.entries(ticket.accounts)) {
          accounts.push({
            id: `${ticket.url}_${account}`,
            url: ticket.url,
            name: ticket.name || '',
            account: account,
            role: role,
            token: ticket.token || '',
            env: ticket.env || '',
            market: ticket.market || ''
          });
        }
      }

      logDebug('Created accounts', accounts);

      ctx.update(c => {
        c.accounts = accounts;
        return c;
      });

      iframeCommStatus.authProcessed = true;
      logDebug('Auth processing completed successfully');

      // 激活第一个账户并跳转到 board
      if (accounts.length > 0) {
        activeAcc(accounts[0]);
        logDebug('Activated first account, navigating to board');
        await goto(localizeHref('/dash/board'));
      } else {
        // 如果没有账户，显示添加机器人界面
        logDebug('No accounts found, showing add bot interface');
        showAdd = true;
      }

    } catch (error) {
      const errorMessage = (error as any).message;
      iframeCommStatus.error = errorMessage;
      logDebug('Auto login failed with error', error);

      if (window.parent) {
        const errorMsg = {
          type: 'AUTH_ERROR',
          error: errorMessage
        };
        window.parent.postMessage(errorMsg, '*');
        logDebug('Sent AUTH_ERROR to parent', errorMsg);
      }
    }
  }

  // 请求父窗口的鉴权数据
  function requestAuthFromParent() {
    if (iframeCommStatus.isInIframe && window.parent) {
      const requestMessage = {
        type: 'REQUEST_AUTH'
      };
      window.parent.postMessage(requestMessage, '*');
      iframeCommStatus.requestSent = true;
      logDebug('Sent REQUEST_AUTH to parent', requestMessage);
    } else {
      logDebug('Cannot request auth from parent', {
        isInIframe: iframeCommStatus.isInIframe,
        hasParent: !!window.parent
      });
    }
  }

  onMount(async () => {
    // 手动检测iframe状态（因为layout可能还没有设置）
    let isInIframe = false;
    try {
      isInIframe = window.self !== window.top;
    } catch (e) {
      isInIframe = true;
    }

    iframeCommStatus.isInIframe = isInIframe;

    logDebug('Component mounted', { isInIframe });

    if (isInIframe) {
      logDebug('Running in iframe mode');
      // 在 iframe 中，监听父窗口消息
      window.addEventListener('message', handleParentMessage);

      // 请求鉴权数据
      requestAuthFromParent();

      // 设置超时检测
      setTimeout(() => {
        if (!iframeCommStatus.authReceived) {
          iframeCommStatus.error = 'Timeout: No response from parent window after 10 seconds';
          logDebug('Auth request timeout');
        }
      }, 10000);
    } else {
      logDebug('Running in standalone mode');
      // 不在 iframe 中，正常加载
      await loadAccounts(true);
    }
  });

  onDestroy(() => {
    if (iframeCommStatus.isInIframe) {
      window.removeEventListener('message', handleParentMessage);
      logDebug('Removed message event listener');
    }
  });
</script>

{#if iframeCommStatus.isInIframe}
<!-- iframe 模式下的特殊界面 -->
<div class="min-h-screen bg-base-100 flex items-center justify-center py-12">
  <div class="container max-w-[600px] mx-auto px-3">
    <div class="text-center space-y-6">
      <!-- Logo -->
      <div class="flex justify-center">
        <img src="/banbot.png" alt="BanBot Logo" class="h-16 object-contain"/>
      </div>

      <!-- 通信状态显示 -->
      <div class="card bg-base-200 shadow-sm">
        <div class="card-body p-6">
          <h2 class="card-title text-lg mb-4 justify-center">Dashboard Communication Status</h2>

          <div class="space-y-3">
            <!-- 基本状态 -->
            <div class="flex items-center justify-between">
              <span class="text-sm">In iframe:</span>
              <div class="badge badge-success badge-sm">✓ Yes</div>
            </div>

            <div class="flex items-center justify-between">
              <span class="text-sm">Auth request sent:</span>
              <div class="badge {iframeCommStatus.requestSent ? 'badge-success' : 'badge-warning'} badge-sm">
                {iframeCommStatus.requestSent ? '✓ Sent' : '⏳ Pending'}
              </div>
            </div>

            <div class="flex items-center justify-between">
              <span class="text-sm">Auth data received:</span>
              <div class="badge {iframeCommStatus.authReceived ? 'badge-success' : 'badge-warning'} badge-sm">
                {iframeCommStatus.authReceived ? '✓ Received' : '⏳ Waiting'}
              </div>
            </div>

            <div class="flex items-center justify-between">
              <span class="text-sm">Auth processed:</span>
              <div class="badge {iframeCommStatus.authProcessed ? 'badge-success' : 'badge-warning'} badge-sm">
                {iframeCommStatus.authProcessed ? '✓ Processed' : '⏳ Processing'}
              </div>
            </div>

            <!-- 错误信息 -->
            {#if iframeCommStatus.error}
            <div class="alert alert-error alert-sm">
              <svg xmlns="http://www.w3.org/2000/svg" class="stroke-current shrink-0 h-4 w-4" fill="none" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 14l2-2m0 0l2-2m-2 2l-2-2m2 2l2 2m7-2a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <span class="text-xs">{iframeCommStatus.error}</span>
            </div>
            {/if}

            <!-- 最后消息时间 -->
            {#if iframeCommStatus.lastMessageTime}
            <div class="text-xs text-base-content/60 text-center">
              Last message: {new Date(iframeCommStatus.lastMessageTime).toLocaleTimeString()}
            </div>
            {/if}
          </div>
        </div>
      </div>

      <!-- Debug信息 (可折叠) -->
      <details class="collapse collapse-arrow bg-base-200">
        <summary class="collapse-title text-sm font-medium">Debug Information</summary>
        <div class="collapse-content">
          <div class="text-xs space-y-2 max-h-60 overflow-y-auto">
            {#each iframeCommStatus.messageHistory.slice(-10) as msg}
            <div class="bg-base-300 p-2 rounded text-left">
              <div class="font-mono text-xs opacity-60">
                {new Date(msg.timestamp).toLocaleTimeString()}
              </div>
              <div class="font-mono text-xs">
                [{msg.type.toUpperCase()}] {JSON.stringify(msg.data, null, 2)}
              </div>
            </div>
            {/each}
          </div>
        </div>
      </details>
    </div>
  </div>
</div>
{:else}
<!-- 正常模式下的界面 -->
<div class="min-h-screen bg-base-100 flex items-center justify-center py-12">
  <div class="container max-w-[1100px] mx-auto px-3">
    <div class="text-center space-y-4 mb-10">
      <h1 class="flex justify-center flex-row">
        <img src="/banbot.png" alt="logo" class="h-14 object-contain"/>
      </h1>
      <div class="space-y-3">
        <p class="text-lg text-base-content/80 my-6">{m.dash_desc()}</p>
        <div class="flex justify-center items-center gap-2 text-sm text-base-content/70">
          <span>{m.dash_help()}</span>
          <a class="link link-primary hover:link-accent transition-colors duration-200"
             href="https://docs.banbot.site/{getLocale()}">{m.our_doc()}</a>
        </div>
      </div>
    </div>

    {#if $save.tickets.length > 0}
    <div class="card bg-base-100 shadow-md border border-base-200 rounded-lg">
      <div class="card-body p-4">
        <div class="tabs tabs-border">
          <input type="radio" name="accounts" class="tab" aria-label={m.trading_accounts()}
                 checked={activeTab === 'accounts'} onclick={() => activeTab = 'accounts'} />
          <div class="tab-content bg-base-100 mt-3">
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
                      <span class="badge badge-sm {accStateClass(acc.status)} font-normal">
                        {accStateText(acc.status)}
                      </span>
                  </td>
                  <td class="text-right font-mono text-sm">{acc.dayDonePft?.toFixed(2)}[{acc.dayDoneNum}]</td>
                  <td class="text-right font-mono text-sm">{acc.dayOpenPft?.toFixed(2)}[{acc.dayOpenNum}]</td>
                  <td>
                    {#if acc.status !== 'disconnected'}
                      <a class="btn btn-xs btn-primary" href={localizeHref("/dash/board")} onclick={() => activeAcc(acc)}>
                        {m.view()}
                      </a>
                    {/if}
                  </td>
                </tr>
              {/each}
              </tbody>
            </table>
          </div>
          <input type="radio" name="tickets" class="tab" aria-label={m.login_credentials()}
                 checked={activeTab === 'tickets'} onclick={() => activeTab = 'tickets'}/>
          <div class="tab-content bg-base-100 mt-3">
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
        </div>
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
{/if}

