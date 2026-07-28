/**
 * The OS adapter, picked ONCE at import time.
 *
 * Never branch on process.platform inside a component or a render function: the
 * point of this seam is that the UI does not know which OS it is on, the same
 * way it does not know how anything is fetched. Everything above this file
 * imports `platform` and reads a field.
 */
import * as macos from "./macos";
import * as windows from "./windows";

export const platform = process.platform === "win32" ? windows : macos;

/** True on Windows. Use only for copy/labels, never for control flow. */
export const isWindows = process.platform === "win32";
