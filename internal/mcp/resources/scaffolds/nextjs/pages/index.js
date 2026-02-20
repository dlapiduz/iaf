import Layout from '../components/Layout';

export default function Home() {
  return (
    <Layout title="Home">
      <div className="max-w-2xl mx-auto">
        <h1 className="text-3xl font-bold text-gray-900 mb-4">Welcome</h1>
        <p className="text-gray-600">
          Your application is running. Edit{' '}
          <code className="bg-gray-100 px-1 rounded">pages/index.js</code> to
          get started.
        </p>
      </div>
    </Layout>
  );
}
