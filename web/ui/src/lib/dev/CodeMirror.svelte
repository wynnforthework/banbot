<script lang="ts" module>
    export type ThemeSpec = Record<string, StyleSpec>;
    export type StyleSpec = {
        [propOrSelector: string]: string | number | StyleSpec | null;
    };
</script>

<script lang="ts">
    import {highlightSpecialChars, drawSelection, highlightActiveLine, dropCursor,
        rectangularSelection, crosshairCursor,
        lineNumbers, highlightActiveLineGutter} from "@codemirror/view"
    import {defaultHighlightStyle, syntaxHighlighting, indentOnInput, bracketMatching,
            foldGutter, foldKeymap} from "@codemirror/language"
    import {defaultKeymap, history, historyKeymap} from "@codemirror/commands"
    import {searchKeymap, highlightSelectionMatches} from "@codemirror/search"
    import {autocompletion, completionKeymap, closeBrackets, closeBracketsKeymap} from "@codemirror/autocomplete"
    import {lintKeymap} from "@codemirror/lint"
    import { onDestroy, onMount } from 'svelte';
    import { EditorView, keymap, placeholder as placeholderExt } from '@codemirror/view';
    import { EditorState, StateEffect, type Extension, type Transaction } from '@codemirror/state';
    import { indentWithTab } from '@codemirror/commands';
    import { indentUnit, type LanguageSupport } from '@codemirror/language';
    import { browser } from '$app/environment';
    import {go} from '@codemirror/lang-go';
    import {yaml} from '@codemirror/lang-yaml';
    import { type KeyBinding } from '@codemirror/view';
    import _ from 'lodash';

    let {
        theme,
        basic = true,
        extensions,
        class: classes,
        tabSize = 4,
        styles,
        lineWrapping = true,
        editable = true,
        readonly = false,
        placeholder,
        debounce_msecs = 1000,
        ready,
        change,
        reconfigure
    }: {
        theme?: Extension;
        basic?: boolean;
        extensions?: Extension[];
        class?: string;
        tabSize?: number;
        styles?: ThemeSpec;
        lineWrapping?: boolean;
        editable?: boolean;
        readonly?: boolean;
        placeholder?: string | HTMLElement | null | undefined;
        debounce_msecs?: number;
        ready?: (view: EditorView) => void;
        change?: (value: string) => void;
        reconfigure?: (view: EditorView) => void;
    } = $props();

    let element: HTMLDivElement | undefined = $state(undefined);
    let view: EditorView;
    let value = $state('');
    let lang: LanguageSupport | undefined = $state(go());
    let useTab = $state(true);

    let update_from_prop = false;
    let first_config = true;

    const basicExts: Extension[] = [
        lineNumbers(),
        highlightActiveLineGutter(),
        highlightSpecialChars(),
        history(),
        foldGutter(),
        drawSelection(),
        dropCursor(),
        EditorState.allowMultipleSelections.of(true),
        indentOnInput(),
        syntaxHighlighting(defaultHighlightStyle, {fallback: true}),
        bracketMatching(),
        closeBrackets(),
        autocompletion(),
        rectangularSelection(),
        crosshairCursor(),
        highlightActiveLine(),
        highlightSelectionMatches(),
        keymap.of([
            ...closeBracketsKeymap,
            ...defaultKeymap,
            ...searchKeymap,
            ...historyKeymap,
            ...foldKeymap,
            ...completionKeymap,
            ...lintKeymap
        ] as KeyBinding[])
    ]

    const debouncedChange = _.debounce((text: string) => {
        change?.(text);
    }, 1000, { trailing: true });

    onMount(() => {
        const exts = calcExtensions();
        view = new EditorView({
            parent: element,
            state: create_editor_state(value, exts),
            dispatch(transaction: Transaction) {
                view.update([transaction]);
                
                if (!update_from_prop && change && transaction.docChanged) {
                    const text = view.state.doc.toString();
                    if (debounce_msecs <= 0) {
                        change(text);
                    } else {
                        debouncedChange(text);
                    }
                }
            }
        });
        ready?.(view);
    });

    onDestroy(() => {
        view?.destroy();
        debouncedChange.cancel();
    });

    export function setValue(name: string, text: string): void {
        const ext = name.split('.').pop()?.toLowerCase() || '';
        if(ext === 'go'){
          lang = go();
        }else if(ext === 'yaml' || ext === 'yml'){
          lang = yaml();
        }else{
          lang = undefined;
        }
        value = text;
        if(!view) {
            return;
        }
        // 这里将text按换行切分，如果行首是\t的行的数量超过行首空格的数量，则将useTab设置为true
        const lines = text.split('\n');
        const tabCount = lines.filter(line => line.startsWith('\t')).length;
        const spaceCount = lines.filter(line => line.startsWith(' ')).length;
        useTab = tabCount > spaceCount;
        const exts = calcExtensions();
        update_from_prop = true;
        view.setState(create_editor_state(value, exts));
        update_from_prop = false;
        console.log('apply update', ext, lang, text.length, useTab);
    }

    function calcExtensions(): Extension[]{
        const exts: Extension[] = [
            indentUnit.of(' '.repeat(tabSize)),
            EditorView.editable.of(editable),
            EditorState.readOnly.of(readonly),
        ];

        if (basic) exts.push(...basicExts);
        if (useTab) exts.push(keymap.of([indentWithTab]));
        if (placeholder) exts.push(placeholderExt(placeholder));
        if (lang) exts.push(lang);
        if (lineWrapping) exts.push(EditorView.lineWrapping);

        if (styles) exts.push(EditorView.theme(styles));
        if (theme) exts.push(theme);
        if (extensions) exts.push(...extensions);
        if(view){
            if (first_config) {
                first_config = false;
                return exts;
            }

            view.dispatch({
                effects: StateEffect.reconfigure.of(exts)
            });

            reconfigure?.(view);
        }
        return exts;
    }

    function create_editor_state(value: string | null | undefined, exts: Extension[]): EditorState {
        return EditorState.create({
            doc: value ?? undefined,
            extensions: exts
        });
    }

</script>

{#if browser}
    <div class="codemirror-wrapper {classes}" bind:this={element} ></div>
{:else}
    <div class="scm-waiting {classes}">
        <div class="scm-waiting__loading scm-loading">
            <div class="scm-loading__spinner" ></div>
            <p class="scm-loading__text">Loading editor...</p>
        </div>

        <pre class="scm-pre cm-editor">{value}</pre>
    </div>
{/if}

<style>
    .codemirror-wrapper {
        outline: none;
        height: 100%;
        overflow-y: auto;
    }

    :global(.codemirror-wrapper .cm-editor) {
        height: 100%;
    }

    :global(.codemirror-wrapper .cm-scroller) {
        overflow: auto;
    }

    .scm-waiting {
        position: relative;
    }
    .scm-waiting__loading {
        position: absolute;
        top: 0;
        left: 0;
        bottom: 0;
        right: 0;
        background-color: rgba(255, 255, 255, 0.5);
    }

    .scm-loading {
        display: flex;
        align-items: center;
        justify-content: center;
    }
    .scm-loading__spinner {
        width: 1rem;
        height: 1rem;
        border-radius: 100%;
        border: solid 2px #000;
        border-top-color: transparent;
        margin-right: 0.75rem;
        animation: spin 1s linear infinite;
    }
    .scm-loading__text {
        font-family: sans-serif;
    }
    .scm-pre {
        font-size: 0.85rem;
        font-family: monospace;
        tab-size: 2;
        -moz-tab-size: 2;
        resize: none;
        pointer-events: none;
        user-select: none;
        overflow: auto;
    }

    @keyframes spin {
        0% {
            transform: rotate(0deg);
        }
        100% {
            transform: rotate(360deg);
        }
    }
</style>
