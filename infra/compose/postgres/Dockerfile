FROM postgres:16-alpine

# Install pg_cron extension
RUN apk add --no-cache \
      build-base \
      clang15 \
      llvm15-dev \
      postgresql16-dev \
  && cd /tmp \
  && wget -qO- https://github.com/citusdata/pg_cron/archive/refs/tags/v1.6.2.tar.gz | tar xz \
  && cd pg_cron-1.6.2 \
  && make && make install \
  && cd / && rm -rf /tmp/pg_cron-1.6.2 \
  && apk del build-base clang15 llvm15-dev postgresql16-dev

# Copy runtime configuration
COPY postgresql.conf /etc/postgresql/postgresql.conf
COPY pg_hba.conf     /etc/postgresql/pg_hba.conf

# pg_cron requires shared_preload_libraries
RUN echo "shared_preload_libraries = 'pg_cron'" >> /etc/postgresql/postgresql.conf \
  && echo "cron.database_name = 'personel'" >> /etc/postgresql/postgresql.conf

USER postgres
EXPOSE 5432
