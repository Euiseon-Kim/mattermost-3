import React from 'react';

// Minimal type definition for plugin registry
interface PluginRegistry {
    registerPostTypeComponent?: (typeName: string, component: React.ComponentType) => void;
}

class Plugin {
    initialize(_registry: PluginRegistry, _store: unknown): void {
        // All core functionality is implemented server-side via slash commands.
        // This webapp entry point is required for Mattermost to load the plugin.
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void;
    }
}

window.registerPlugin('com.mattermost.gitlab-review', new Plugin());
