import { createContext } from "react";
import type { AccountView } from "./types";

// Shared by the startup screen and Settings so signing out unmounts the workspace.
export const AccountSessionContext = createContext<((view: AccountView) => void) | null>(null);
