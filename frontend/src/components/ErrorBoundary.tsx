import React from "react";

interface Props {
  children: React.ReactNode;
  resetKey?: string | number;
}

interface State {
  hasError: boolean;
  message: string;
}

export class ErrorBoundary extends React.Component<Props, State> {
  state: State = { hasError: false, message: "" };

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, message: error?.message || "Неизвестная ошибка интерфейса" };
  }

  componentDidUpdate(prevProps: Props) {
    if (this.state.hasError && prevProps.resetKey !== this.props.resetKey) {
      this.setState({ hasError: false, message: "" });
    }
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="empty-state" role="alert" style={{ margin: "32px auto", maxWidth: 520 }}>
          <h3>Произошла ошибка отображения</h3>
          <p style={{ marginTop: 6 }}>{this.state.message}</p>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            style={{ marginTop: 14 }}
            onClick={() => this.setState({ hasError: false, message: "" })}
          >
            Восстановить
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
