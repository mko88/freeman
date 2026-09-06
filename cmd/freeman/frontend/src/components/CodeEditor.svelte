<script lang="ts">
  // A CodeMirror 6 editor themed to look like the rest of Freeman.
  //
  // Used for the request's raw body, where the editing behaviours are
  // worth a dependency: indentation, bracket matching, folding, and an
  // undo history that survives programmatic edits — which a plain
  // textarea can't offer, because writing its value back from Svelte
  // clears the browser's own undo stack.
  //
  // The read-only highlighting elsewhere (the Code tab, the response
  // pane) stays hand-rolled: it costs nothing and this buys it nothing.
  import { onDestroy, onMount } from 'svelte'
  import { indentWithTab, temporarilySetTabFocusMode } from '@codemirror/commands'
  import { json } from '@codemirror/lang-json'
  import { xml } from '@codemirror/lang-xml'
  import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
  import { Compartment, EditorState } from '@codemirror/state'
  import { EditorView, keymap, placeholder as placeholderExt } from '@codemirror/view'
  import { tags } from '@lezer/highlight'
  import { basicSetup } from 'codemirror'

  export let value: string
  export let language: 'json' | 'xml' | 'plain' = 'plain'
  export let placeholder = ''

  let host: HTMLElement
  let view: EditorView | undefined

  // Language and theme are swapped at runtime, so they go behind
  // compartments rather than being baked into the initial state.
  const languageCompartment = new Compartment()

  // The same five colours the hand-rolled highlighters use (see
  // style.css) mapped onto lezer's tags, so a JSON body reads
  // identically whether it's in this editor or the response pane.
  const freemanHighlight = HighlightStyle.define([
    { tag: [tags.propertyName, tags.tagName], color: 'var(--fm-accent)' },
    { tag: [tags.string, tags.attributeValue], color: 'var(--fm-success)' },
    { tag: [tags.number, tags.attributeName], color: 'var(--fm-method-get)' },
    { tag: [tags.bool, tags.atom], color: 'var(--fm-method-put)' },
    { tag: [tags.null, tags.comment, tags.angleBracket], color: 'var(--fm-text-muted)' },
    { tag: tags.invalid, color: 'var(--fm-error)' },
  ])

  const freemanTheme = EditorView.theme(
    {
      '&': {
        height: '100%',
        fontSize: '0.9rem',
        backgroundColor: 'var(--fm-bg-elevated)',
        color: 'var(--fm-text)',
      },
      '&.cm-focused': { outline: 'none' },
      '.cm-scroller': {
        fontFamily: "'IBM Plex Mono', 'Cascadia Code', Consolas, monospace",
        lineHeight: '1.5',
      },
      '.cm-content': { caretColor: 'var(--fm-text)' },
      '.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--fm-text)' },
      '.cm-gutters': {
        backgroundColor: 'transparent',
        color: 'var(--fm-text-muted)',
        border: 'none',
      },
      '.cm-activeLine': { backgroundColor: 'var(--fm-bg-hover)' },
      '.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--fm-text)' },
      '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
        backgroundColor: 'var(--fm-bg-hover)',
      },
      '.cm-matchingBracket, &.cm-focused .cm-matchingBracket': {
        backgroundColor: 'color-mix(in srgb, var(--fm-accent) 25%, transparent)',
        outline: 'none',
      },
      '.cm-placeholder': { color: 'var(--fm-text-muted)' },
      '.cm-panels, .cm-tooltip': {
        backgroundColor: 'var(--fm-bg-panel)',
        color: 'var(--fm-text)',
        border: '1px solid var(--fm-border)',
      },
    },
    { dark: true },
  )

  function languageExtension(lang: typeof language) {
    if (lang === 'json') return json()
    if (lang === 'xml') return xml()
    return []
  }

  onMount(() => {
    view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          // Tab indents. Escape then Tab gets you out: tab-focus mode is
          // CodeMirror's own answer to Tab-capture trapping the
          // keyboard, and lasts until the next keypress. The default
          // keymap only reaches it via Ctrl-m, which nobody guesses.
          keymap.of([indentWithTab, { key: 'Escape', run: temporarilySetTabFocusMode }]),
          languageCompartment.of(languageExtension(language)),
          freemanTheme,
          syntaxHighlighting(freemanHighlight),
          EditorView.lineWrapping,
          placeholderExt(placeholder),
          EditorView.updateListener.of((update) => {
            if (update.docChanged) {
              value = update.state.doc.toString()
            }
          }),
        ],
      }),
    })
  })

  onDestroy(() => view?.destroy())

  // Push external changes in (selecting a different request), but only
  // when they really differ — echoing the editor's own update back would
  // reset the cursor on every keystroke.
  $: if (view && value !== view.state.doc.toString()) {
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: value } })
  }

  $: if (view) {
    view.dispatch({ effects: languageCompartment.reconfigure(languageExtension(language)) })
  }
</script>

<div class="code-editor" bind:this={host}></div>

<style>
  .code-editor {
    height: 220px;
    min-height: 120px;
    resize: vertical;
    overflow: hidden;
    border: 1px solid var(--fm-border);
    border-radius: var(--fm-radius);
    background: var(--fm-bg-elevated);
  }

  .code-editor:focus-within {
    border-color: var(--fm-accent);
  }
</style>
