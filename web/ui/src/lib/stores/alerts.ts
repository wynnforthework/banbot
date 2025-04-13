import { writable } from 'svelte/store';

export type AlertType = 'info' | 'success' | 'warning' | 'error';

export interface AlertMessage {
    type: AlertType;
    text: string;
    secs: number;
    id: number;
}

export function createAlertStore() {
    const { subscribe, update } = writable<AlertMessage[]>([]);
    let nextId = 1;
    function addAlert(type: AlertType, text: string, secs: number = 3)  {
        const id = nextId++;
        update((alerts: AlertMessage[]) => [...alerts, { type, text, secs, id }]);

        // 设定时间后自动移除
        setTimeout(() => {
            update((alerts: AlertMessage[]) => alerts.filter((alert: AlertMessage) => alert.id !== id));
        }, secs * 1000);
    }
    function error(text: string, secs: number = 3){
        addAlert('error', text, secs)
    }
    function success(text: string, secs: number = 3){
        addAlert('success', text, secs)
    }
    function warning(text: string, secs: number = 3){
        addAlert('warning', text, secs)
    }
    function info(text: string, secs: number = 3){
        addAlert('info', text, secs)
    }
    return {
        subscribe, addAlert, error, success, warning, info,
        removeAlert: (id: number) => {
            update((alerts: AlertMessage[]) => alerts.filter((alert: AlertMessage) => alert.id !== id));
        }
    };
}

export const alerts = createAlertStore(); 