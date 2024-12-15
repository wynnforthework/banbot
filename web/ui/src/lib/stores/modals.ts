import { writable } from 'svelte/store';

export interface ModalMessage {
    text: string;
    title: string;
    buttons: string[];
    id: number;
    resolve: (value: string) => void;
}

export function createModalStore() {
    const { subscribe, update } = writable<ModalMessage[]>([]);
    let nextId = 1;

    return {
        subscribe,
        alert: async (text: string, title: string = '') => {
            return new Promise<void>((resolve) => {
                const id = nextId++;
                update(modals => [...modals, { 
                    text, 
                    title,
                    buttons: ['confirm'], 
                    id,
                    resolve: (value: string) => {
                        update(modals => modals.filter(modal => modal.id !== id));
                        resolve();
                    }
                }]);
            });
        },
        confirm: async (text: string, title: string = ''): Promise<boolean> => {
            return new Promise<boolean>((resolve) => {
                const id = nextId++;
                console.log('confirm', id, text, title);
                update(modals => [...modals, { 
                    text, 
                    title,
                    buttons: ['confirm', 'cancel'], 
                    id,
                    resolve: (value: string) => {
                        update(modals => modals.filter(modal => modal.id !== id));
                        resolve(value === 'confirm');
                    }
                }]);
            });
        }
    };
}

export const modals = createModalStore(); 