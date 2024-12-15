import { get, writable } from 'svelte/store';
import { persisted } from 'svelte-persisted-store';
import type { BotTicket, BotAccount } from './types';
import { DashCtx, DashSave } from './context';
import { getAccApi } from '$lib/netio';
import {site} from '$lib/config';

// 创建全局状态stores
export const ctx = writable<DashCtx>(new DashCtx());
export const save = persisted('dash', new DashSave());
export const acc = writable<BotAccount>({
  url: '',
  name: '',
  account: '',
  role: '',
  token: '',
})

// 更新账户信息
export async function updateAcc(accInfo: BotAccount) {
  site.update(s => {
    s.apiHost = accInfo.url;
    s.apiReady = true;
    return s;
  });
  acc.set(accInfo);
  const rsp = await getAccApi('/today_num');
  if (rsp.code === 200) {
      accInfo.running = rsp.running;
      accInfo.dayDoneNum = rsp.dayDoneNum;
      accInfo.dayDonePft = rsp.dayDonePft;
      accInfo.dayOpenNum = rsp.dayOpenNum;
      accInfo.dayOpenPft = rsp.dayOpenPft;
  } else {
      console.error('update acc status fail', acc, rsp)
  }
}

// 加载机器人账户信息
export async function loadBotAccounts(bot: BotTicket, request: boolean = false) {
  if (!bot.accounts) return;
  const accounts = get(ctx).accounts;
  
  // 创建一个Promise数组来存储所有异步操作
  const promises = Object.entries(bot.accounts!).map(async ([account, role]) => {
    const existingIdx = accounts.findIndex(a => 
        a.url === bot.url && a.account === account
    );

    const accInfo: BotAccount = {
        url: bot.url,
        name: bot.name || '',
        account: account,
        role: role,
        token: bot.token || ''
    };

    if (existingIdx >= 0) {
        // 如果新角色是admin且旧角色不是admin，则更新
        const old = accounts[existingIdx];
        if (role === 'admin' && old.role !== 'admin') {
            old.role = 'admin';
            old.token = bot.token || old.token;
        }
    } else {
        // 新账户直接添加
        if (request) {
            await updateAcc(accInfo)
        }
        accounts.push(accInfo);
    }
  });
  // 等待所有异步操作完成
  await Promise.all(promises);
  ctx.update(c => {
      c.accounts = accounts;
      return c;
  });
} 

export async function loadAccounts(request: boolean = false) {
  const saveShot = get(save);
  const tickets = saveShot.tickets;
  await Promise.all(tickets.map(async bot => await loadBotAccounts(bot, request)));
  const accounts = get(ctx).accounts;
  const selected = accounts.filter(a => `${a.url}_${a.account}` === saveShot.current);
  if (selected.length > 0) {
    acc.set(selected[0]);
  }else if(accounts.length > 0){
    acc.set(accounts[0]);
    save.update(s => {
      s.current = `${accounts[0].url}_${accounts[0].account}`;
      return s;
    });
  }else{
    console.log('no accounts avaiable', saveShot);
  }
}