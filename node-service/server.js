const express = require('express');
const sqlite3 = require('sqlite3').verbose();
const axios = require('axios');
const cheerio = require('cheerio');
const cors = require('cors');

const app = express();
const PORT = process.env.PORT || 3000;
const DATABASE = process.env.DATABASE || './node.db';

// Middleware
app.use(cors());
app.use(express.json());

// Initialize database
const db = new sqlite3.Database(DATABASE, (err) => {
    if (err) {
        console.error('Error opening database:', err);
    } else {
        console.log('Connected to SQLite database');
        initDatabase();
    }
});

function initDatabase() {
    db.run(`
        CREATE TABLE IF NOT EXISTS metadata (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            short_code TEXT UNIQUE NOT NULL,
            url TEXT NOT NULL,
            title TEXT,
            description TEXT,
            favicon_url TEXT,
            fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `, (err) => {
        if (err) {
            console.error('Error creating table:', err);
        } else {
            console.log('Database initialized successfully');
        }
    });
}

// Fetch metadata for a URL
async function fetchUrlMetadata(url) {
    try {
        // Fetch the HTML content
        const response = await axios.get(url, {
            timeout: 5000,
            headers: {
                'User-Agent': 'Mozilla/5.0 (compatible; URLShortenerBot/1.0)'
            },
            maxRedirects: 5
        });

        const html = response.data;
        const $ = cheerio.load(html);

        // Extract title
        let title = $('title').text().trim();
        if (!title) {
            title = $('meta[property="og:title"]').attr('content');
        }
        if (!title) {
            title = 'No title found';
        }

        // Extract description
        let description = $('meta[name="description"]').attr('content');
        if (!description) {
            description = $('meta[property="og:description"]').attr('content');
        }
        if (!description) {
            description = 'No description available';
        }

        // Extract favicon
        let faviconUrl = null;
        
        // Try various favicon selectors
        const faviconSelectors = [
            'link[rel="icon"]',
            'link[rel="shortcut icon"]',
            'link[rel="apple-touch-icon"]',
            'link[rel="icon"][type="image/png"]',
            'link[rel="icon"][type="image/x-icon"]'
        ];

        for (const selector of faviconSelectors) {
            faviconUrl = $(selector).attr('href');
            if (faviconUrl) break;
        }

        // If no favicon found, use default
        if (!faviconUrl) {
            const urlObj = new URL(url);
            faviconUrl = `${urlObj.protocol}//${urlObj.hostname}/favicon.ico`;
        } else if (faviconUrl.startsWith('//')) {
            // Protocol-relative URL
            const urlObj = new URL(url);
            faviconUrl = `${urlObj.protocol}${faviconUrl}`;
        } else if (faviconUrl.startsWith('/')) {
            // Convert root-relative URL to absolute
            const urlObj = new URL(url);
            faviconUrl = `${urlObj.protocol}//${urlObj.hostname}${faviconUrl}`;
        } else if (!faviconUrl.startsWith('http')) {
            // Convert relative URL to absolute
            const urlObj = new URL(url);
            faviconUrl = `${urlObj.protocol}//${urlObj.hostname}/${faviconUrl}`;
        }

        return {
            title: title.substring(0, 200), // Limit title length
            description: description.substring(0, 500), // Limit description length
            favicon_url: faviconUrl
        };

    } catch (error) {
        console.error('Error fetching metadata:', error.message);
        return {
            title: 'Unable to fetch title',
            description: 'Could not retrieve page information',
            favicon_url: null
        };
    }
}

// POST /api/metadata - Fetch and store metadata
app.post('/api/metadata', async (req, res) => {
    const { short_code, long_url } = req.body;

    if (!short_code || !long_url) {
        return res.status(400).json({ error: 'short_code and long_url are required' });
    }

    console.log(`Fetching metadata for: ${long_url}`);

    // Fetch metadata from the URL
    const metadata = await fetchUrlMetadata(long_url);

    // Store in database
    db.run(
        `INSERT OR REPLACE INTO metadata (short_code, url, title, description, favicon_url)
         VALUES (?, ?, ?, ?, ?)`,
        [short_code, long_url, metadata.title, metadata.description, metadata.favicon_url],
        function(err) {
            if (err) {
                console.error('Database error:', err);
                return res.status(500).json({ error: 'Failed to store metadata' });
            }

            console.log(`✅ Metadata stored for ${short_code}: ${metadata.title}`);
            
            res.json({
                short_code,
                url: long_url,
                ...metadata,
                status: 'success'
            });
        }
    );
});

// GET /api/metadata/:short_code - Retrieve stored metadata
app.get('/api/metadata/:short_code', (req, res) => {
    const { short_code } = req.params;

    db.get(
        'SELECT * FROM metadata WHERE short_code = ?',
        [short_code],
        (err, row) => {
            if (err) {
                console.error('Database error:', err);
                return res.status(500).json({ error: 'Database error' });
            }

            if (!row) {
                return res.status(404).json({ error: 'Metadata not found' });
            }

            res.json(row);
        }
    );
});

// GET /api/metadata - Get all metadata
app.get('/api/metadata', (req, res) => {
    db.all('SELECT * FROM metadata ORDER BY fetched_at DESC', (err, rows) => {
        if (err) {
            console.error('Database error:', err);
            return res.status(500).json({ error: 'Database error' });
        }

        res.json({ metadata: rows, count: rows.length });
    });
});

// Health check endpoint
app.get('/health', (req, res) => {
    res.json({ 
        status: 'healthy', 
        service: 'metadata-service',
        timestamp: new Date().toISOString()
    });
});

if (require.main === module) {
    const server = app.listen(PORT, () => {
        console.log(`🚀 Node.js Metadata Service running on http://localhost:${PORT}`);
        console.log(`📊 Database: ${DATABASE}`);
    });

    // Graceful shutdown
    process.on('SIGINT', () => {
        console.log('\nShutting down gracefully...');
        server.close(() => {
            db.close((err) => {
                if (err) {
                    console.error('Error closing database:', err);
                }
                process.exit(0);
            });
        });
    });
}

module.exports = { app, db, fetchUrlMetadata };

