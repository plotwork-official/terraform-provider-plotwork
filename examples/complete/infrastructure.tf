# A small stack for the example to draw. Replace it with your own.

resource "aws_vpc" "main" {
  cidr_block = "10.20.0.0/16"
  tags       = { Name = "${var.environment}-vpc" }
}

resource "aws_subnet" "app" {
  for_each          = { a = "10.20.1.0/24", b = "10.20.2.0/24" }
  vpc_id            = aws_vpc.main.id
  cidr_block        = each.value
  availability_zone = "${var.region}${each.key}"
}

resource "aws_lb" "app" {
  name               = "${var.environment}-app"
  load_balancer_type = "application"
  subnets            = [for s in aws_subnet.app : s.id]
}

resource "aws_db_subnet_group" "main" {
  name       = "${var.environment}-db"
  subnet_ids = [for s in aws_subnet.app : s.id]
}

resource "aws_db_instance" "main" {
  identifier                  = "${var.environment}-db"
  engine                      = "postgres"
  instance_class              = "db.t4g.micro"
  allocated_storage           = 20
  username                    = "app"
  manage_master_user_password = true
  db_subnet_group_name        = aws_db_subnet_group.main.name
  skip_final_snapshot         = true
}
