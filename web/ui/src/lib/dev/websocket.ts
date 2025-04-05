import { site } from "$lib/stores/site";
import { get } from "svelte/store";

let ws: WebSocket | undefined;
const callbacks: Record<string, (res: Record<string, any>) => void> = {};
const listeners: Record<string, ((res: Record<string, any>) => void)[]> = {};
const btPrgs: Record<number, number> = {};
let msgId = 0;

/**
 * 添加消息监听器
 * @param key 监听器key
 * @param callback 回调函数
 */
export function addListener(key: string, callback: (res: Record<string, any>) => void) {
    if (!listeners[key]) {
        listeners[key] = [];
    }
    listeners[key].push(callback);
}

/**
 * 初始化WebSocket连接
 * @param cb 连接成功后的回调函数
 */
export function initWsConn(cb?: () => void) {
    if (ws?.readyState === WebSocket.OPEN) return;

    const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws';
    let host = get(site).apiHost;
    if (!host) {
        host = location.host;
    } else {
        host = host.replace(/^https?:\/\//, '');
    }

    const path = '/api/dev/ws';
    const wsUrl = `${protocol}://${host}${path}`;
    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        console.log(`WebSocket ${path} connected`);
        cb?.();
        sendWsMsg({action: 'status'});
    };

    ws.onmessage = (event) => {
        try {
            const result = JSON.parse(event.data);
            // 如果响应中包含id，则调用对应的回调函数
            if (result.id && callbacks[result.id]) {
                callbacks[result.id](result);
                delete callbacks[result.id];
            }else if (result.type === 'status'){
              site.update(s => {
                Object.assign(s, result.data);
                return s;
              });
            }else if (result.type === 'heavyPrg'){
                site.update(s => {
                    s.heavyName = result.name
                    s.heavyProgress = result.progress
                    return s;
                });
            } else if (result.type === 'btPrg') {
                const key = `btPrg_${result.taskId}`;
                if (listeners[key]) {
                    listeners[key].forEach(callback => callback(result));
                }
                if (result.progress >= 0.99999) {
                    delete btPrgs[result.taskId];
                }else{
                    btPrgs[result.taskId] = result.progress;
                }
                let siteShot = get(site);
                if(siteShot.heavyName == "" || siteShot.heavyName == "backtest"){
                    let done = 0;
                    for(let prg of Object.values(btPrgs)){
                        done += prg;
                    }
                    let total = Object.keys(btPrgs).length;
                    let name = 'backtest';
                    if (total == 0){
                        done = 0;
                        name = "";
                    }
                    site.update(s => {
                        s.heavyName = name;
                        s.heavyProgress = done/total;
                        return s;
                    });
                }
            } else if(result.type){
              console.log(`ws dev unknown msg:`, result);
            }
        } catch (err) {
            console.error(`Failed to parse WebSocket ${path} message:`, err);
        }
    };

    ws.onerror = (error) => {
        console.error(`WebSocket ${path} error:`, error);
    };

    ws.onclose = () => {
        console.log(`WebSocket ${path} disconnected`);
    };
}


/**
 * 发送WebSocket消息
 * @param msg 要发送的消息对象
 * @param cb 接收响应的回调函数
 */
export function sendWsMsg(msg: Record<string, any>, cb?: (res: Record<string, any>) => void) {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        console.error('WebSocket is not connected');
        return;
    }

    // 为消息添加唯一ID
    const id = `msg_${++msgId}`;
    const message = { ...msg, id };

    if (cb) {
        callbacks[id] = cb;
    }

    ws.send(JSON.stringify(message));
} 