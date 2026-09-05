// The Wails-transport implementation of $backend (see vite.config.ts's
// alias) — a straight re-export of the generated bindings. Desktop-only;
// backend.http.ts is the same contract for the web build.
export {
  CurrentWorkspace,
  OpenWorkspace,
  SelectWorkspaceFolder,
  GetCollection,
  SaveRequest,
  DeleteRequest,
  GetEnvironment,
  SaveEnvironment,
  ExecuteRequest,
  GetTheme,
  GetResponseBody,
  OpenResponseExternally,
  OpenResponseInFileExplorer,
  GetCachedResponse,
  ClearResponseCache,
  OpenResponseCacheExternally,
  GetResponseCachePath,
  OpenResponseCacheInFileExplorer,
} from '../wailsjs/go/wailsapp/App.js'
