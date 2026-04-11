FROM postgres:16-bookworm

# Install pg_cron extension.
#
# We used to build pg_cron from source on alpine, but that path was
# brittle: Alpine's pgxs expects a versioned clang-N binary (clang-19
# for postgres:16-alpine 2026 builds) that isn't reliably packaged.
# Debian's PGDG apt repository ships prebuilt postgresql-16-cron, so
# this switch trades ~200MB of image size for build reproducibility.
# Fixed 2026-04-11 after three rounds of apk/pgxs debugging.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      postgresql-16-cron \
      locales \
 && rm -rf /var/lib/apt/lists/*

# Generate Turkish + en_US locales so initdb --locale=tr_TR.UTF-8 succeeds.
# postgres:16-bookworm base does not ship any non-C locales by default.
RUN sed -i 's/^# *\(tr_TR.UTF-8 UTF-8\)/\1/' /etc/locale.gen \
 && sed -i 's/^# *\(en_US.UTF-8 UTF-8\)/\1/' /etc/locale.gen \
 && locale-gen tr_TR.UTF-8 en_US.UTF-8
ENV LANG=tr_TR.UTF-8 LC_ALL=tr_TR.UTF-8

# Copy runtime configuration
COPY postgresql.conf /etc/postgresql/postgresql.conf
COPY pg_hba.conf     /etc/postgresql/pg_hba.conf

# pg_cron requires shared_preload_libraries
RUN echo "shared_preload_libraries = 'pg_cron'" >> /etc/postgresql/postgresql.conf \
  && echo "cron.database_name = 'personel'" >> /etc/postgresql/postgresql.conf

USER postgres
EXPOSE 5432
