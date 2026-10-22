FROM node:22.23.3-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.28.2-alpine
COPY nginx-main.conf /etc/nginx/nginx.conf
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY nginx-http.conf /etc/nginx/aiq/http.conf
COPY nginx-server.conf /etc/nginx/aiq/server.conf
COPY nginx-pilot.conf.template /etc/nginx/aiq/pilot.conf.template
COPY start-pilot.sh /app/start-pilot.sh
COPY --from=build /src/dist /usr/share/nginx/html
EXPOSE 80
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
