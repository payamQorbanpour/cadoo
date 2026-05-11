import * as vscode from 'vscode';

// Phase 6 ships the scaffold + two commands. Real local-review behaviour
// lands in Phase 6.x once cadoo-api exposes a /v1/review endpoint that
// accepts a unified diff over HTTP.

export function activate(context: vscode.ExtensionContext): void {
  const apiUrl = (): string =>
    vscode.workspace.getConfiguration('cadoo').get<string>('apiUrl') ?? 'http://localhost:8080';

  context.subscriptions.push(
    vscode.commands.registerCommand('cadoo.review', async () => {
      const editor = vscode.window.activeTextEditor;
      if (!editor) {
        vscode.window.showWarningMessage('Cadoo: no active editor.');
        return;
      }
      // TODO(phase-6.x): POST file/diff to `${apiUrl()}/v1/review` and render
      // findings as diagnostics in the Problems panel.
      vscode.window.showInformationMessage(
        `Cadoo: would review ${editor.document.uri.fsPath} via ${apiUrl()} (not yet implemented).`,
      );
    }),
    vscode.commands.registerCommand('cadoo.config.validate', async () => {
      const ws = vscode.workspace.workspaceFolders?.[0];
      if (!ws) {
        vscode.window.showWarningMessage('Cadoo: no workspace open.');
        return;
      }
      const path = vscode.Uri.joinPath(ws.uri, '.cadoo.yaml');
      try {
        const stat = await vscode.workspace.fs.stat(path);
        vscode.window.showInformationMessage(
          `Cadoo: .cadoo.yaml found (${stat.size} bytes). Full validation lands in Phase 6.x.`,
        );
      } catch {
        vscode.window.showWarningMessage('Cadoo: no .cadoo.yaml at workspace root.');
      }
    }),
  );
}

export function deactivate(): void {}
