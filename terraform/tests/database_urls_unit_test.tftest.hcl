# modules/database-urls refuses the admin URLs prototype 4-2 found broken (spec 4 §5.12).

variables {
  admin_url = "postgres://pscale_api_test.br4nch:test-admin@db.example.com:5432/goal?sslmode=verify-full"
  passwords = {
    owner = "owner0000000000000000000000000000000"
    app   = "app00000000000000000000000000000000"
    auth  = "auth0000000000000000000000000000000"
  }
  goald_pool_max_conns = 8
}

run "a_direct_verified_url_on_goals_database_passes" {
  command = plan

  module {
    source = "./modules/database-urls"
  }
}

run "pgbouncer_port_is_refused" {
  command = plan

  module {
    source = "./modules/database-urls"
  }

  variables {
    admin_url = "postgres://pscale_api_test.br4nch:test-admin@db.example.com:6432/goal?sslmode=verify-full"
  }

  expect_failures = [var.admin_url]
}

run "the_providers_default_database_is_refused" {
  command = plan

  module {
    source = "./modules/database-urls"
  }

  variables {
    admin_url = "postgres://pscale_api_test.br4nch:test-admin@db.example.com:5432/postgres?sslmode=verify-full"
  }

  expect_failures = [var.admin_url]
}

run "a_user_without_the_branch_is_refused" {
  command = plan

  module {
    source = "./modules/database-urls"
  }

  variables {
    admin_url = "postgres://pscale_api_test:test-admin@db.example.com:5432/goal?sslmode=verify-full"
  }

  expect_failures = [var.admin_url]
}

run "an_unverified_connection_is_refused" {
  command = plan

  module {
    source = "./modules/database-urls"
  }

  variables {
    admin_url = "postgres://pscale_api_test.br4nch:test-admin@db.example.com:5432/goal?sslmode=require"
  }

  expect_failures = [var.admin_url]
}

run "a_password_that_needs_escaping_is_refused" {
  command = plan

  module {
    source = "./modules/database-urls"
  }

  variables {
    passwords = {
      owner = "owner0000000000000000000000000000000"
      app   = "p@ss/word:with#reserved?characters!!"
      auth  = "auth0000000000000000000000000000000"
    }
  }

  expect_failures = [var.passwords]
}
