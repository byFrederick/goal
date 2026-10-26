# modules/database-urls refuses the inputs that would build a URL prototype 4-2 found broken
# (spec 4 §5.12).

variables {
  host      = "db.example.com"
  database  = "goal"
  branch_id = "br4nch"
  admin = {
    username = "pscale_api_test.br4nch"
    password = "test-admin"
  }
  passwords = {
    owner = "owner"
    app   = "app"
    auth  = "auth"
  }
  goald_pool_max_conns = 8
}

run "valid_inputs_pass" {
  command = plan

  module {
    source = "./modules/database-urls"
  }

  assert {
    condition     = output.auth_url == "postgres://goal_auth.br4nch:auth@db.example.com:5432/goal?sslmode=verify-full"
    error_message = "The auth URL names port 5432, database goal and verify-full only."
  }
}

run "a_host_with_a_port_is_refused" {
  command = plan

  module {
    source = "./modules/database-urls"
  }

  variables {
    host = "db.example.com:6432"
  }

  expect_failures = [var.host]
}

run "the_providers_default_database_is_refused" {
  command = plan

  module {
    source = "./modules/database-urls"
  }

  variables {
    database = "postgres"
  }

  expect_failures = [var.database]
}

run "an_empty_branch_id_is_refused" {
  command = plan

  module {
    source = "./modules/database-urls"
  }

  variables {
    branch_id = ""
  }

  expect_failures = [var.branch_id]
}
