const express = require('express');
const path = require('path');

const app = express();
const port = process.env.PORT || 8080;

// Serve static files from the public directory.
app.use(express.static(path.join(__dirname, 'public')));

// Health endpoint — must return 200 before any page loads.
app.get('/health', (req, res) => {
  res.status(200).json({ status: 'ok' });
});

// Fallback: serve index.html for all other GET requests.
app.get('*', (req, res) => {
  res.sendFile(path.join(__dirname, 'public', 'index.html'));
});

app.listen(port, () => {
  console.log(`Server listening on port ${port}`);
});
