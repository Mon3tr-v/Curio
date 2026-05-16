import React from 'react';

type ErrorBoundaryState = { error: Error | null };

export class ErrorBoundary extends React.Component<React.PropsWithChildren, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error) {
    return { error };
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <main className="fatalShell">
        <section className="fatalPanel">
          <span>页面渲染失败</span>
          <h1>Curio 遇到了一条异常数据</h1>
          <p>{this.state.error.message}</p>
          <button className="primary" onClick={() => window.location.reload()} type="button">
            刷新页面
          </button>
        </section>
      </main>
    );
  }
}
