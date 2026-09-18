-- +goose Up
UPDATE account SET role = 'user' WHERE role = 'app_manager';
UPDATE enrollment SET template_role = 'user' WHERE template_role = 'app_manager';

ALTER TABLE account DROP CONSTRAINT account_role_check;
ALTER TABLE account ADD CONSTRAINT account_role_check
  CHECK (role IN ('user', 'admin'));
ALTER TABLE enrollment DROP CONSTRAINT enrollment_template_role_check;
ALTER TABLE enrollment ADD CONSTRAINT enrollment_template_role_check
  CHECK (template_role IN ('user', 'admin'));

-- Application assignments are independent of account roles and remain intact.

-- +goose Down
-- Former app_manager roles cannot be reconstructed after conversion to user.
ALTER TABLE account DROP CONSTRAINT account_role_check;
ALTER TABLE account ADD CONSTRAINT account_role_check
  CHECK (role IN ('user', 'app_manager', 'admin'));
ALTER TABLE enrollment DROP CONSTRAINT enrollment_template_role_check;
ALTER TABLE enrollment ADD CONSTRAINT enrollment_template_role_check
  CHECK (template_role IN ('user', 'app_manager', 'admin'));
