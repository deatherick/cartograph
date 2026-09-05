class ValidationError(Exception):
    pass


def require_positive(value, field_name):
    if value <= 0:
        raise ValidationError(field_name)


def require_non_negative(value, field_name):
    if value < 0:
        raise ValidationError(field_name)
