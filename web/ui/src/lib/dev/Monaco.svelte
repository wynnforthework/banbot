<script lang="ts">
import { onMount, onDestroy } from 'svelte';
import { browser } from '$app/environment';

/*
monaco-editor是从vscode提取的编辑器。

参考代码： https://www.npmjs.com/package/monaco-languageclient-examples?activeTab=code
path : /monaco-languageclient-examples/src/groovy/client/main.ts

依赖："monaco-editor-wrapper": "^6.0.0-next.11",

https://microsoft.github.io/monaco-editor/docs.html

此组件尝试使用monaco-editor-wrapper来对monaco-editor提供语言服务支持、自动完成、语法检查、代码格式化等功能
但实际启动时一直websocket连接失败，gopls端错误： invalid header line "GET / HTTP/1.1"
monaco-editor-wrapper尚没有golang端的支持示例，猜测需要做一些额外工作才能使用，暂时放弃
*/

let { 
    class: className = '', 
    change,
}: {
    class?: string;
    change?: (path: string, value: string) => void;
} = $props();

let container: HTMLDivElement;
let wrapper: any;
let editor: any;
let monaco: any;
let initValue: string = '';
let initPath: string = '';

// 文件管理相关状态
interface FileModel {
    path: string;
    content: string;
    language: string;
    model: any;
}

let files = new Map<string, FileModel>();
let currentFile: string | null = null;

// 定义语言映射常量
const LANGUAGE_MAP: Record<string, string> = {
    'go': 'go',
    'java': 'java',
    'md': 'markdown',
    'txt': 'plaintext',
    'py': 'python',
    'js': 'javascript',
    'ts': 'typescript',
    'json': 'json',
    'yaml': 'yaml',
    'yml': 'yaml',
    'html': 'html',
    'css': 'css',
    'sql': 'sql'
};

// 修改后的 getLanguageFromPath 函数
function getLanguageFromPath(path: string): string {
    const ext = path.split('.').pop()?.toLowerCase() || '';
    return LANGUAGE_MAP[ext] || 'plaintext';
}

function setFileModel(path: string, value: string) {
    if(!files.has(path)) {
        const lang = getLanguageFromPath(path);
        const model = monaco.editor.createModel(value, lang, monaco.Uri.parse(path));
        files.set(path, { path, content: value, language: lang, model });
    }
    const file = files.get(path);
    if(file) {
        editor.setModel(file.model);
        currentFile = path;
    }
}

export function openFile(path: string, value: string) {
    console.log('open file', path, value.length, editor, monaco)
    if (editor && monaco) {
        setFileModel(path, value);
    }else{
        initValue = value;
        initPath = path;
    }
}

export function getOpenFiles(): string[] {
    return Array.from(files.keys());
}

export function getActive(): string | null {
    return currentFile;
}

export function closeFile(path?: string) {
    if (path) {
        const file = files.get(path);
        if (file) {
            file.model.dispose();
        }
        files.delete(path);
        if (currentFile === path) {
            currentFile = files.size > 0 ? Array.from(files.keys())[0] : null;
        }
    } else {
        files.forEach(file => {
            file.model.dispose();
        });
        files.clear();
        currentFile = null;
    }
}

async function initMonaco() {
    if (!browser) return;

    const wrapperMod = await import('monaco-editor-wrapper');
    const { MonacoEditorLanguageClientWrapper } = wrapperMod;
    type WrapperConfig = typeof wrapperMod.WrapperConfig;
    monaco = await import('monaco-editor');
    const { useWorkerFactory } = await import('monaco-editor-wrapper/workerFactory');
    
    const configureMonacoWorkers = (logger?: any) => {
        useWorkerFactory({
            workerOverrides: {
                ignoreMapping: true,
                workerLoaders: {
                    TextEditorWorker: () => new Worker(new URL('monaco-editor/esm/vs/editor/editor.worker.js', import.meta.url), { type: 'module' }),
                    TextMateWorker: () => new Worker(new URL('@codingame/monaco-vscode-textmate-service-override/worker', import.meta.url), { type: 'module' })
                }
            },
            logger
        });
    };

    const helloJavaCode = `
public static void main (String[] args) {
    System.out.println("Hello World!");
}`;

    const eclipseJdtLsConfig = {
        port: 30003,
        path: '/jdtls',
        basePath: '/home/mlc/packages/examples/resources/eclipse.jdt.ls'
    };


    const config: WrapperConfig = {
        $type: 'extended',
        htmlContainer: container,
        vscodeApiConfig: {
            serviceOverrides: {
                //...getKeybindingsServiceOverride(),
            },
            userConfiguration: {
                json: JSON.stringify({
                    'workbench.colorTheme': 'Default Dark Modern',
                    'editor.guides.bracketPairsHorizontal': 'active',
                    'editor.wordBasedSuggestions': 'off',
                    'editor.experimental.asyncTokenization': true,
                    //'go.useLanguageServer': true,
                })
            }
        },
        editorAppConfig: {
            // codeResources: {
            //     modified: {
            //         text: helloJavaCode,
            //         uri: `${eclipseJdtLsConfig.basePath}/workspace/hello.java`
            //     }
            // },
            // codeResources: {
            //     modified: {text: initValue, enforceLanguageId: initLang}
            // },
            monacoWorkerFactory: configureMonacoWorkers
        },
        languageClientConfigs: {
            // java: {
            //     connection: {
            //         options: {
            //             $type: 'WebSocketUrl',
            //             url: 'ws://localhost:30003/jdtls'
            //         }
            //     },
            //     clientOptions: {
            //         documentSelector: ['java'],
            //         workspaceFolder: {
            //             index: 0,
            //             name: 'workspace',
            //             uri: monaco.Uri.parse(`${eclipseJdtLsConfig.basePath}/workspace`)
            //         }
            //     }
            // },
            go: {
                connection: {
                    options: {
                        $type: 'WebSocketUrl',
                        name: 'golang language server',
                        url: 'ws://localhost:37374',
                        startOptions: {
                            onCall: () => {
                                console.log('Connected to socket.');
                            },
                            reportStatus: true
                        },
                        stopOptions: {
                            onCall: () => {
                                console.log('Disconnected from socket.');
                            },
                            reportStatus: true
                        }
                    }
                },
                clientOptions: {
                    documentSelector: ["go"],
                    workspaceFolder: {
                        index: 0,
                        name: "workspace",
                        uri: monaco.Uri.parse("file:///E:/trade/banbot"),
                    },
                },
            }
        }
    }

    wrapper = new MonacoEditorLanguageClientWrapper();
    await wrapper.initAndStart(config);
    editor = wrapper.getEditor();

    if(initPath && initValue) {
        setFileModel(initPath, initValue);
    }

    editor.onDidChangeModelContent(() => {
        const model = editor.getModel();
        if(!model) return;
        const uri = model.uri.toString();
        if (uri) {
            change?.(uri, model.getValue());
        }
    });
}

onMount(initMonaco);

onDestroy(async () => {
    if (wrapper) {
        await wrapper.dispose();
    }
    files.clear();
});
</script>

<div bind:this={container} class="flex-1 {className}"></div>
