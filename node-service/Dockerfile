FROM node:24.11-alpine

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm install

# Copy application file
COPY server.js ./

# Expose port
EXPOSE 3000

# Run node
CMD ["node", "server.js"]

