export default function SignIn() {
  return (
    <div className="center">
      <div className="card">
        <h1>NeuralVault</h1>
        <p className="hint">Sign in to build and query your own knowledge base.</p>
        {/* Full navigation so the browser follows the OIDC redirect to the provider. */}
        <a className="btn" href="/api/auth/login">
          Sign in
        </a>
      </div>
    </div>
  );
}
