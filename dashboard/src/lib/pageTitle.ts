/** buildTitle composes the document title: "<page> · <instance>", or just the
 * instance name when a route has no page title. */
export function buildTitle(pageName: string, instanceName: string): string {
  return pageName ? `${pageName} · ${instanceName}` : instanceName
}
