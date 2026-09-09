<script lang="ts">
  // A CodeMirror 6 editor themed to look like the rest of Freeman.
  //
  // Serves the request's raw body (editable) and the response body
  // (readOnly), so both get the same grammar-driven highlighting,
  // folding, and viewport rendering from one theme.
  //
  // The Code tab uses the same grammars, one per format it can
  // generate. They come from @codemirror/legacy-modes rather than the
  // lang- packages: those grammars are already a dependency, and none of
  // these four needs the extra fidelity — a generated script is short
  // and its shape is known.
  import { onDestroy, onMount } from 'svelte'
  import { indentWithTab, temporarilySetTabFocusMode } from '@codemirror/commands'
  import { json } from '@codemirror/lang-json'
  import { xml } from '@codemirror/lang-xml'
  import { HighlightStyle, StreamLanguage, syntaxHighlighting } from '@codemirror/language'
  import { javascript } from '@codemirror/legacy-modes/mode/javascript'
  import { powerShell } from '@codemirror/legacy-modes/mode/powershell'
  import { python } from '@codemirror/legacy-modes/mode/python'
  import { shell } from '@codemirror/legacy-modes/mode/shell'
  import { Compartment, EditorState } from '@codemirror/state'
  import { EditorView, keymap, placeholder as placeholderExt } from '@codemirror/view'
  import { tags } from '@lezer/highlight'
  import { basicSetup } from 'codemirror'

  export let value: string
  export let language: 'json' | 'xml' | 'shell' | 'powershell' | 'python' | 'javascript' | 'plain' = 'plain'
  export let placeholder = ''
  // Read-only mode is the response pane and the Code tab: same
  // highlighting, same theme, plus folding and viewport rendering, which
  // matter more there than in a body you type — a large response used to
  // render as one <pre> full of spans.
  export let readOnly = false
  // How the editor is sized: a fixed box the user can drag (the request
  // body), or whatever height the pane has left (the response pane and
  // the Code tab, each of which owns the space below the tab bar).
  export let layout: 'box' | 'fill' = 'box'
  // Off for generated commands, which scroll sideways rather than wrap —
  // a wrapped one-line curl is harder to read, not easier.
  export let wrap = true

  let host: HTMLElement
  let view: EditorView | undefined

  // Language and theme are swapped at runtime, so they go behind
  // compartments rather than being baked into the initial state.
  const languageCompartment = new Compartment()

  // The same five colours the hand-rolled highlighters use (see
  // style.css) mapped onto lezer's tags, so a JSON body reads
  // identically whether it's in this editor or the response pane.
  const freemanHighlight = HighlightStyle.define([
    { tag: [tags.propertyName, tags.tagName, tags.keyword, tags.definitionKeyword], color: 'var(--fm-accent)' },
    { tag: [tags.string, tags.attributeValue, tags.special(tags.string)], color: 'var(--fm-success)' },
    { tag: [tags.number, tags.attributeName, tags.standard(tags.variableName)], color: 'var(--fm-method-get)' },
    { tag: [tags.bool, tags.atom, tags.variableName], color: 'var(--fm-method-put)' },
    { tag: [tags.null, tags.comment, tags.angleBracket, tags.meta], color: 'var(--fm-text-muted)' },
    { tag: tags.invalid, color: 'var(--fm-error)' },
  ])

  const freemanTheme = EditorView.theme(
    {
      '&': {
        height: '100%',
        // One size for both: the request body and the response body are
        // the same kind of reading, and sitting one above the other made
        // the difference between them look like a mistake.
        fontSize: '0.85rem',
        backgroundColor: 'transparent',
        color: 'var(--fm-text)',
      },
      '&.cm-focused': { outline: 'none' },
      '.cm-scroller': {
        overflow: 'auto',
        // The token, not a literal stack: Settings -> Appearance can
        // replace the fixed-width face, and CodeMirror is the one place
        // the font is set from JavaScript rather than CSS.
        fontFamily: "var(--fm-font-mono)",
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
      // Ctrl-F's panel. basicSetup binds it already; what it lacked was
      // any of this — CodeMirror's stock controls are a light-theme
      // field and a grey gradient button, which against the app's own
      // panel read as a rendering fault rather than a search box.
      '.cm-panel.cm-search': {
        padding: '6px 8px',
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        gap: '6px',
        fontSize: '0.8rem',
      },
      '.cm-panel.cm-search label': {
        display: 'inline-flex',
        alignItems: 'center',
        gap: '3px',
        color: 'var(--fm-text-muted)',
        fontSize: '0.75rem',
      },
      '.cm-panel.cm-search .cm-textfield': {
        backgroundColor: 'var(--fm-bg)',
        color: 'var(--fm-text)',
        border: '1px solid var(--fm-border)',
        borderRadius: 'var(--fm-radius)',
        padding: '2px 6px',
        // The stock field is sized in ems off a font it no longer has.
        fontSize: 'inherit',
        fontFamily: 'inherit',
      },
      '.cm-panel.cm-search .cm-textfield:focus': {
        outline: 'none',
        borderColor: 'var(--fm-accent)',
      },
      '.cm-panel.cm-search .cm-button': {
        backgroundColor: 'var(--fm-bg-elevated)',
        backgroundImage: 'none',
        color: 'var(--fm-text)',
        border: '1px solid var(--fm-border)',
        borderRadius: 'var(--fm-radius)',
        padding: '2px 8px',
        fontSize: 'inherit',
        fontFamily: 'inherit',
        cursor: 'pointer',
      },
      '.cm-panel.cm-search .cm-button:hover': { backgroundColor: 'var(--fm-bg-hover)' },
      '.cm-panel.cm-search [name="close"]': {
        color: 'var(--fm-text-muted)',
        cursor: 'pointer',
        fontSize: '1rem',
        padding: '0 4px',
      },
      '.cm-panel.cm-search [name="close"]:hover': { color: 'var(--fm-text)' },
      // What the search actually found. highlightSelectionMatches comes
      // with basicSetup too, and had the same problem: no colour of its
      // own here meant the matches were invisible.
      '.cm-searchMatch': {
        backgroundColor: 'color-mix(in srgb, var(--fm-warning) 30%, transparent)',
        outline: '1px solid color-mix(in srgb, var(--fm-warning) 55%, transparent)',
      },
      '.cm-searchMatch.cm-searchMatch-selected': {
        backgroundColor: 'color-mix(in srgb, var(--fm-accent) 45%, transparent)',
      },
      '.cm-selectionMatch': {
        backgroundColor: 'color-mix(in srgb, var(--fm-accent) 22%, transparent)',
      },
    },
    { dark: true },
  )

  function languageExtension(lang: typeof language) {
    if (lang === 'json') return json()
    if (lang === 'xml') return xml()
    if (lang === 'shell') return StreamLanguage.define(shell)
    if (lang === 'powershell') return StreamLanguage.define(powerShell)
    if (lang === 'python') return StreamLanguage.define(python)
    if (lang === 'javascript') return StreamLanguage.define(javascript)
    return []
  }

  onMount(() => {
    view = new EditorView({
      parent: host,
      state: EditorState.create({
        doc: value,
        extensions: [
          basicSetup,
          ...(readOnly ? [EditorState.readOnly.of(true), EditorView.editable.of(false)] : []),
          // Tab indents. Escape then Tab gets you out: tab-focus mode is
          // CodeMirror's own answer to Tab-capture trapping the
          // keyboard, and lasts until the next keypress. The default
          // keymap only reaches it via Ctrl-m, which nobody guesses.
          keymap.of([indentWithTab, { key: 'Escape', run: temporarilySetTabFocusMode }]),
          languageCompartment.of(languageExtension(language)),
          freemanTheme,
          syntaxHighlighting(freemanHighlight),
          ...(wrap ? [EditorView.lineWrapping] : []),
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

<div class="code-editor {layout}" bind:this={host}></div>

<style>
  /* 220px preferred, but it grows into whatever the pane isn't using —
     so collapsing the response gives the body editor the space rather
     than leaving a gap under it. The request/response splitter is what
     sizes this now, which is why there's no resize handle of its own. */
  .code-editor.box {
    flex: 1 1 220px;
    min-height: 120px;
    overflow: hidden;
    border: 1px solid var(--fm-border);
    border-radius: var(--fm-radius);
    background: var(--fm-bg-elevated);
  }

  /* Sizes itself to what's left of the pane rather than to a handle, so
     it drops the fixed height and the border it doesn't need. */
  .code-editor.fill {
    flex: 1;
    min-height: 0;
    background: var(--fm-bg-response);
  }

  .code-editor.box:focus-within {
    border-color: var(--fm-accent);
  }
</style>
