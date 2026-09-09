// The Wails-transport implementation of $backend (see vite.config.ts's
// alias) — a straight re-export of the generated bindings. Desktop-only;
// backend.http.ts is the same contract for the web build.
export {
  CurrentWorkspace,
  OpenWorkspace,
  SelectWorkspaceFolder,
  GetCollection,
  CreateCollection,
  RenameCollection,
  DeleteCollection,
  SaveRequest,
  DeleteRequest,
  GetEnvironment,
  SaveEnvironment,
  DeleteEnvironment,
  ExecuteRequest,
  GenerateRequestCode,
  GetTheme,
  GetVersion,
  CancelRequest,
  GetSettings,
  SaveSettings,
  GetCookies,
  DeleteCookie,
  ClearCookies,
  GetCachedResponse,
  ClearResponseCache,
  ClearCachedResponse,
  OpenResponseCacheExternally,
  GetResponseCachePath,
  GetResponseCacheDataURI,
  OpenResponseCacheInFileExplorer,
} from '../wailsjs/go/wailsapp/App.js'

// The window itself, from the Wails runtime rather than a bound Go
// method — there is no Go side to these. Needed because the desktop
// build is frameless (see cmd/freeman/main.go), so the app draws the
// controls the OS title bar used to provide.
export {
  WindowMinimise as MinimiseWindow,
  WindowToggleMaximise as ToggleMaximiseWindow,
  WindowIsMaximised as IsWindowMaximised,
  Quit as CloseWindow,
} from '../wailsjs/runtime/runtime.js'

// Whether this bundle is the desktop app. The web build has a browser
// window around it, so it renders no title bar of its own. Annotated
// rather than inferred: backend.contract.ts asserts both modules have
// the same shape, and the literal types `true` and `false` are not.
export const IS_DESKTOP: boolean = true
