import { Minus, PanelLeft, PanelRight, Search, Square, X } from "lucide-react";
import { TabBar } from "./TabBar";
import type { TabMeta } from "../lib/types";
import { useT } from "../lib/i18n";

export type DesktopPlatform = "darwin" | "windows" | "linux";

interface WindowControlsProps {
  platform: DesktopPlatform;
  className?: string;
}

interface AppChromeProps {
  platform: DesktopPlatform;
  workbenchChrome?: boolean;
  tabs: TabMeta[];
  activeTabId?: string;
  revealActiveSignal: number;
  commandCompact: boolean;
  sidebarTogglePressed: boolean;
  sidebarExpandBlocked: boolean;
  sidebarCollapsed: boolean;
  sidebarToggleTitle: string;
  workspacePanelMaximized: boolean;
  workspacePanelRenderable: boolean;
  workspaceTogglePressed: boolean;
  workspacePanelLabel: string;
  onToggleSidebar: () => void;
  onToggleWorkspacePanel: () => void;
  onTabChange: (tabId: string) => void;
  onTabClose: (tabId: string) => void;
  onTabsClose: (tabIds: string[], nextActiveTabId?: string) => void;
  onTabsReorder: (tabIds: string[]) => void;
  onNewTab: () => void;
  onOpenPalette: () => void;
}

export function WindowControls({ platform, className = "" }: WindowControlsProps) {
  const t = useT();
  const darwinChrome = platform === "darwin";
  const windowControlsClassName = [
    "app-chrome__window-controls",
    `app-chrome__window-controls--${platform}`,
    className,
  ].filter(Boolean).join(" ");
  const handleWindowMinimize = () => {
    if (typeof window === "undefined") return;
    window.runtime?.WindowMinimise?.();
  };
  const handleWindowMaximize = () => {
    if (typeof window === "undefined") return;
    window.runtime?.WindowToggleMaximise?.();
  };
  const handleWindowClose = () => {
    if (typeof window === "undefined") return;
    window.runtime?.Quit?.();
  };

  return (
    <div className={windowControlsClassName} role="group" aria-label={t("window.controls")}>
      {darwinChrome ? (
        <>
          <button
            className="app-chrome__window-control app-chrome__window-control--close"
            type="button"
            onClick={handleWindowClose}
            aria-label={t("window.close")}
            title={t("window.close")}
          >
            <X size={8} strokeWidth={2.4} />
          </button>
          <button
            className="app-chrome__window-control app-chrome__window-control--minimize"
            type="button"
            onClick={handleWindowMinimize}
            aria-label={t("window.minimize")}
            title={t("window.minimize")}
          >
            <Minus size={8} strokeWidth={2.4} />
          </button>
          <button
            className="app-chrome__window-control app-chrome__window-control--maximize"
            type="button"
            onClick={handleWindowMaximize}
            aria-label={t("window.maximize")}
            title={t("window.maximize")}
          >
            <Square size={7} strokeWidth={2.2} />
          </button>
        </>
      ) : (
        <>
          <button
            className="app-chrome__window-control app-chrome__window-control--minimize"
            type="button"
            onClick={handleWindowMinimize}
            aria-label={t("window.minimize")}
            title={t("window.minimize")}
          >
            <Minus size={12} strokeWidth={1.9} />
          </button>
          <button
            className="app-chrome__window-control app-chrome__window-control--maximize"
            type="button"
            onClick={handleWindowMaximize}
            aria-label={t("window.maximize")}
            title={t("window.maximize")}
          >
            <Square size={10} strokeWidth={1.8} />
          </button>
          <button
            className="app-chrome__window-control app-chrome__window-control--close"
            type="button"
            onClick={handleWindowClose}
            aria-label={t("window.close")}
            title={t("window.close")}
          >
            <X size={12} strokeWidth={1.9} />
          </button>
        </>
      )}
    </div>
  );
}

export function AppChrome({
  platform,
  workbenchChrome = false,
  tabs,
  activeTabId,
  revealActiveSignal,
  commandCompact,
  sidebarTogglePressed,
  sidebarExpandBlocked,
  sidebarCollapsed,
  sidebarToggleTitle,
  workspacePanelMaximized,
  workspacePanelRenderable,
  workspaceTogglePressed,
  workspacePanelLabel,
  onToggleSidebar,
  onToggleWorkspacePanel,
  onTabChange,
  onTabClose,
  onTabsClose,
  onTabsReorder,
  onNewTab,
  onOpenPalette,
}: AppChromeProps) {
  const t = useT();
  const darwinChrome = platform === "darwin";
  const showWindowControls = true;
  const chromeClassName = [
    "app-chrome",
    "app-chrome--tabs",
    darwinChrome ? "app-chrome--darwin-tabs" : "app-chrome--native-tabs",
    workbenchChrome ? "app-chrome--workbench" : "",
    !darwinChrome ? "app-chrome--identityless" : "",
    `app-chrome--platform-${platform}`,
  ].filter(Boolean).join(" ");
  const tabBar = (
    <TabBar
      tabs={tabs}
      activeTabId={activeTabId}
      revealActiveSignal={revealActiveSignal}
      onTabChange={onTabChange}
      onTabClose={onTabClose}
      onTabsClose={onTabsClose}
      onTabsReorder={onTabsReorder}
      onNewTab={onNewTab}
      onOpenPalette={undefined}
      commandCompact={commandCompact}
    />
  );

  return (
    <header className={chromeClassName}>
      {darwinChrome && <span className="app-chrome__drag-rail" aria-hidden="true" />}
      <button
        className={[
          "app-chrome__panel-toggle",
          "app-chrome__panel-toggle--left",
          sidebarTogglePressed ? "app-chrome__panel-toggle--pressed" : "",
          sidebarExpandBlocked ? "app-chrome__panel-toggle--blocked" : "",
        ].filter(Boolean).join(" ")}
        type="button"
        onClick={sidebarExpandBlocked ? undefined : onToggleSidebar}
        aria-label={sidebarToggleTitle}
        aria-pressed={!sidebarCollapsed}
        aria-disabled={sidebarExpandBlocked}
      >
        <PanelLeft size={16} />
      </button>
      {workbenchChrome && (
        <button
          className="app-chrome__workbench-search"
          type="button"
          onClick={onOpenPalette}
          aria-label={t("palette.placeholder")}
        >
          <Search size={18} />
        </button>
      )}

      {workbenchChrome ? (
        <span className="app-chrome__spacer" aria-hidden="true" />
      ) : darwinChrome ? (
        <>
          <div className="app-chrome__tab-strip app-chrome__tab-strip--darwin">
            {tabBar}
          </div>
          <div
            className={[
              "app-chrome__tools",
              "app-chrome__tools--fixed",
              workspaceTogglePressed ? "app-chrome__tools--workspace-pressed" : "",
            ].filter(Boolean).join(" ")}
            aria-label={t("tabBar.commandSearch")}
          >
            <button
              className={[
                "tabbar__command",
                "tabbar__command--compact",
                "app-chrome__command",
              ].filter(Boolean).join(" ")}
              type="button"
              onClick={onOpenPalette}
              aria-label={t("palette.placeholder")}
              title={t("palette.placeholder")}
            >
              <Search size={16} className="tabbar__command-icon" />
            </button>
          </div>
        </>
      ) : (
        <>
          <div className="app-chrome__tab-strip app-chrome__tab-strip--native">
            {tabBar}
          </div>
          <div
            className={[
              "app-chrome__tools",
              workspaceTogglePressed ? "app-chrome__tools--workspace-pressed" : "",
            ].filter(Boolean).join(" ")}
            aria-label={t("tabBar.commandSearch")}
          >
            <button
              className={[
                "tabbar__command",
                "tabbar__command--compact",
                "app-chrome__command",
              ].filter(Boolean).join(" ")}
              type="button"
              onClick={onOpenPalette}
              aria-label={t("palette.placeholder")}
              title={t("palette.placeholder")}
            >
              <Search size={16} className="tabbar__command-icon" />
            </button>
          </div>
        </>
      )}

      {!workspacePanelMaximized && (
        <button
          className={[
            "app-chrome__panel-toggle",
            "app-chrome__panel-toggle--right",
            workspacePanelRenderable ? "app-chrome__panel-toggle--active" : "",
            workspaceTogglePressed ? "app-chrome__panel-toggle--pressed" : "",
          ].filter(Boolean).join(" ")}
          type="button"
          onClick={onToggleWorkspacePanel}
          aria-label={workspacePanelLabel}
          aria-pressed={workspacePanelRenderable}
        >
          <PanelRight size={16} />
        </button>
      )}
      {showWindowControls && <WindowControls platform={platform} />}
    </header>
  );
}
